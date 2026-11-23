package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Acesso de ATENDENTE: usuário e senha criados pelo administrador (chave-mestra),
// com acesso só às sessões liberadas para ele e só à superfície de chamadas
// (atender, ligar, espera, transferir, histórico). O login devolve um token
// HMAC-assinado com expiração, usado no lugar da API key (X-API-Key / ?apiKey=),
// então o painel e os transportes de áudio (SSE/WS) funcionam sem mudança.

const (
	agentTokenPrefix = "at1"
	agentTokenTTL    = 12 * time.Hour
	agentMaxFails    = 5                // tentativas erradas seguidas antes de travar
	agentLockFor     = 15 * time.Minute // tempo travado
	agentMinPassword = 8
)

type agent struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Sessions []string `json:"sessions"`
	hash     []byte
}

func (a *agent) canSession(sid string) bool {
	for _, s := range a.Sessions {
		if s == sid {
			return true
		}
	}
	return false
}

// pwTag identifica a senha atual no token: trocar a senha invalida os tokens antigos.
func (a *agent) pwTag() string {
	sum := sha256.Sum256(a.hash)
	return hex.EncodeToString(sum[:6])
}

type agentTokenClaims struct {
	ID  string `json:"id"`
	PW  string `json:"pw"`
	Exp int64  `json:"exp"`
}

func agentSigningSecret() []byte {
	mac := hmac.New(sha256.New, []byte(os.Getenv("WACALLS_API_KEY")))
	mac.Write([]byte("wacalls-agent-token-v1"))
	return mac.Sum(nil)
}

type loginFail struct {
	n     int
	until time.Time
}

// agentDirectory guarda os atendentes em memória (são poucos) e persiste no banco
// principal. db nil = só memória (testes).
type agentDirectory struct {
	mu    sync.RWMutex
	byID  map[string]*agent
	fails map[string]*loginFail
	db    *sql.DB
	now   func() time.Time
}

var errAgentInput = errors.New("dados inválidos")

func newAgentDirectory(ctx context.Context, db *sql.DB) (*agentDirectory, error) {
	d := &agentDirectory{byID: map[string]*agent{}, fails: map[string]*loginFail{}, db: db, now: time.Now}
	if db == nil {
		return d, nil
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agents (
		id         TEXT PRIMARY KEY,
		username   TEXT NOT NULL UNIQUE,
		name       TEXT NOT NULL,
		pass_hash  TEXT NOT NULL,
		sessions   TEXT NOT NULL DEFAULT '[]',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, username, name, pass_hash, sessions FROM agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a agent
		var hash, sess string
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &hash, &sess); err != nil {
			return nil, err
		}
		a.hash = []byte(hash)
		_ = json.Unmarshal([]byte(sess), &a.Sessions)
		d.byID[a.ID] = &a
	}
	return d, rows.Err()
}

func (d *agentDirectory) save(ctx context.Context, a *agent) error {
	if d.db == nil {
		return nil
	}
	sess, _ := json.Marshal(a.Sessions)
	_, err := d.db.ExecContext(ctx, `INSERT INTO agents (id, username, name, pass_hash, sessions)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, pass_hash=EXCLUDED.pass_hash, sessions=EXCLUDED.sessions`,
		a.ID, a.Username, a.Name, string(a.hash), string(sess))
	return err
}

var usernameRe = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

func hashPassword(pw string) ([]byte, error) {
	if len(pw) < agentMinPassword {
		return nil, errors.New("a senha precisa ter pelo menos 8 caracteres")
	}
	return bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
}

func (d *agentDirectory) list() []agent {
	d.mu.RLock()
	out := make([]agent, 0, len(d.byID))
	for _, a := range d.byID {
		out = append(out, *a)
	}
	d.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

func (d *agentDirectory) get(id string) (agent, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.byID[id]
	if !ok {
		return agent{}, false
	}
	return *a, true
}

func (d *agentDirectory) create(ctx context.Context, username, name, password string, sessions []string) (agent, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameRe.MatchString(username) {
		return agent{}, errors.New("usuário: 3 a 32 caracteres (letras minúsculas, números, . _ -)")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return agent{}, err
	}
	a := &agent{ID: generateRandomString(16), Username: username, Name: strings.TrimSpace(name), Sessions: cleanSessions(sessions), hash: hash}
	if a.Name == "" {
		a.Name = username
	}
	d.mu.Lock()
	for _, x := range d.byID {
		if x.Username == username {
			d.mu.Unlock()
			return agent{}, errors.New("já existe um atendente com esse usuário")
		}
	}
	d.byID[a.ID] = a
	d.mu.Unlock()
	if err := d.save(ctx, a); err != nil {
		d.mu.Lock()
		delete(d.byID, a.ID)
		d.mu.Unlock()
		return agent{}, err
	}
	return *a, nil
}

// update altera nome, senha e/ou sessões (nil = mantém).
func (d *agentDirectory) update(ctx context.Context, id string, name, password *string, sessions *[]string) (agent, error) {
	d.mu.RLock()
	cur, ok := d.byID[id]
	var next agent
	if ok {
		next = *cur
	}
	d.mu.RUnlock()
	if !ok {
		return agent{}, errAgentNotFound
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		next.Name = strings.TrimSpace(*name)
	}
	if password != nil {
		hash, err := hashPassword(*password)
		if err != nil {
			return agent{}, err
		}
		next.hash = hash
	}
	if sessions != nil {
		next.Sessions = cleanSessions(*sessions)
	}
	if err := d.save(ctx, &next); err != nil {
		return agent{}, err
	}
	d.mu.Lock()
	d.byID[id] = &next
	d.mu.Unlock()
	return next, nil
}

var errAgentNotFound = errors.New("atendente não encontrado")

func (d *agentDirectory) remove(ctx context.Context, id string) error {
	d.mu.Lock()
	_, ok := d.byID[id]
	delete(d.byID, id)
	d.mu.Unlock()
	if !ok {
		return errAgentNotFound
	}
	if d.db != nil {
		_, err := d.db.ExecContext(ctx, `DELETE FROM agents WHERE id=$1`, id)
		return err
	}
	return nil
}

func cleanSessions(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// dummyHash é usado quando o usuário não existe, para o tempo de resposta não
// revelar quais usuários existem.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("wacalls-dummy-password"), bcrypt.DefaultCost)

var (
	errLoginLocked = errors.New("muitas tentativas erradas; tente de novo mais tarde")
	errLoginBad    = errors.New("usuário ou senha inválidos")
)

// login confere usuário/senha com limite de tentativas por usuário (agentMaxFails
// erros seguidos travam o usuário por agentLockFor) e devolve um token.
func (d *agentDirectory) login(username, password string) (string, agent, time.Time, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	now := d.now()
	d.mu.Lock()
	if f := d.fails[username]; f != nil && now.Before(f.until) {
		d.mu.Unlock()
		return "", agent{}, time.Time{}, errLoginLocked
	}
	var found *agent
	for _, a := range d.byID {
		if a.Username == username {
			cp := *a
			found = &cp
			break
		}
	}
	d.mu.Unlock()

	hash := dummyHash
	if found != nil {
		hash = found.hash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || found == nil {
		d.mu.Lock()
		f := d.fails[username]
		if f == nil || !f.until.IsZero() && !now.Before(f.until) {
			f = &loginFail{}
			d.fails[username] = f
		}
		f.n++
		if f.n >= agentMaxFails {
			f.until = now.Add(agentLockFor)
		}
		d.mu.Unlock()
		return "", agent{}, time.Time{}, errLoginBad
	}
	d.mu.Lock()
	delete(d.fails, username)
	d.mu.Unlock()
	tok, exp := d.issue(found)
	return tok, *found, exp, nil
}

// issue emite um token novo para o atendente (login e renovação).
func (d *agentDirectory) issue(a *agent) (string, time.Time) {
	exp := d.now().Add(agentTokenTTL)
	return signHMACToken(agentTokenPrefix, agentSigningSecret(), agentTokenClaims{ID: a.ID, PW: a.pwTag(), Exp: exp.Unix()}), exp
}

// fromToken valida um token de atendente: assinatura, expiração, atendente ainda
// existente e senha não trocada desde o login.
func (d *agentDirectory) fromToken(tok string) (*agent, bool) {
	if d == nil || !strings.HasPrefix(tok, agentTokenPrefix+".") {
		return nil, false
	}
	var c agentTokenClaims
	if !parseHMACToken(agentTokenPrefix, agentSigningSecret(), tok, &c) || c.Exp <= d.now().Unix() {
		return nil, false
	}
	a, ok := d.get(c.ID)
	if !ok || a.pwTag() != c.PW {
		return nil, false
	}
	return &a, true
}

// agentAllowedCallActions são as operações de chamada liberadas ao atendente em
// /api/sessions/{sid}/calls/{id}/{ação}. Ficam de fora: espionar (spy), toque
// fantasma (fake) e chamada em grupo.
var agentAllowedCallActions = map[string]bool{
	"accept": true, "reject": true, "webrtc": true, "ws": true, "hold": true, "resume": true,
	"mute": true, "transfer": true, "pickup": true, "volume": true, "silence": true, "video": true,
}

// agentAllowed diz se o atendente pode chamar esta rota: só a superfície de
// chamadas das sessões liberadas para ele.
func agentAllowed(r *http.Request, a *agent) bool {
	p := r.URL.Path
	switch p {
	case "/api/config", "/api/events":
		return r.Method == http.MethodGet
	case "/api/agent/refresh":
		return r.Method == http.MethodPost
	case "/api/sessions": // a lista é filtrada no handler
		return r.Method == http.MethodGet
	}
	rest, ok := strings.CutPrefix(p, "/api/sessions/")
	if !ok {
		return false
	}
	segs := strings.Split(rest, "/")
	if len(segs) < 2 || !a.canSession(segs[0]) {
		return false
	}
	segs = segs[1:]
	switch {
	case len(segs) == 1 && segs[0] == "history":
		return r.Method == http.MethodGet
	case len(segs) == 1 && segs[0] == "calls":
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	case segs[0] != "calls" || segs[1] == "fake" || segs[1] == "group":
		return false
	case len(segs) == 2:
		return r.Method == http.MethodDelete // desligar
	case len(segs) == 3:
		return agentAllowedCallActions[segs[2]]
	case len(segs) == 4:
		return segs[2] == "video" && r.Method == http.MethodPost
	}
	return false
}

type agentCtxKey struct{}

func withAgentCtx(r *http.Request, a *agent) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), agentCtxKey{}, a))
}

// agentFrom devolve o atendente da requisição (nil = administrador/widget).
func agentFrom(r *http.Request) *agent {
	a, _ := r.Context().Value(agentCtxKey{}).(*agent)
	return a
}

// ---------- handlers ----------

// POST /api/agent/login {username, password} — aberto (é o próprio login).
func (s *server) handleAgentLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload inválido"})
		return
	}
	tok, a, exp, err := s.agents.login(body.Username, body.Password)
	switch {
	case errors.Is(err, errLoginLocked):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp.UTC().Format(time.RFC3339), "agent": a})
}

// POST /api/agent/refresh — o atendente troca o token válido por um novo (mais
// 12 h). O painel chama periodicamente, então a aba aberta não "expira" no meio do
// expediente; atendente removido ou com senha trocada não renova.
func (s *server) handleAgentRefresh(w http.ResponseWriter, r *http.Request) {
	a := agentFrom(r)
	if a == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "só para atendente"})
		return
	}
	tok, exp := s.agents.issue(a)
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp.UTC().Format(time.RFC3339), "agent": a})
}

// agentWatchEvery: de quanto em quanto tempo o SSE de um atendente confere se o
// token ainda vale (atendente removido, senha trocada, token expirado).
var agentWatchEvery = 30 * time.Second

// agentStreamContext devolve um contexto que é cancelado quando o token do
// atendente deixa de valer — o SSE fecha e o painel volta para o login.
func (s *server) agentStreamContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	tok := requestAPIKey(r)
	go func() {
		t := time.NewTicker(agentWatchEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, ok := s.agents.fromToken(tok); !ok {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

// GET /api/agents — só chave-mestra.
func (s *server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": s.agents.list()})
}

type agentInput struct {
	Username string    `json:"username"`
	Name     *string   `json:"name"`
	Password *string   `json:"password"`
	Sessions *[]string `json:"sessions"`
}

// POST /api/agents {username, name, password, sessions[]} — só chave-mestra.
func (s *server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Password == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username e password são obrigatórios"})
		return
	}
	name, sessions := "", []string{}
	if in.Name != nil {
		name = *in.Name
	}
	if in.Sessions != nil {
		sessions = *in.Sessions
	}
	a, err := s.agents.create(r.Context(), in.Username, name, *in.Password, sessions)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"agent": a})
}

// PUT /api/agents/{id} {name?, password?, sessions?} — só chave-mestra.
func (s *server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload inválido"})
		return
	}
	a, err := s.agents.update(r.Context(), r.PathValue("id"), in.Name, in.Password, in.Sessions)
	switch {
	case errors.Is(err, errAgentNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"agent": a})
	}
}

// DELETE /api/agents/{id} — só chave-mestra. Os tokens do atendente param na hora.
func (s *server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if err := s.agents.remove(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
