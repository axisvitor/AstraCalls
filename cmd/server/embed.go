package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow/types"
)

// Aba "Ligações" dentro da conversa do Chatwoot (Dashboard App oficial): o Chatwoot
// carrega /embed/chatwoot num iframe e manda o contexto da conversa por
// postMessage. O servidor só guarda a configuração (qual caixa de entrada usa qual
// sessão, quais atributos do contato têm o número) e define quem pode embutir a
// página. Nada aqui chama a API do Chatwoot.

// defaultEmbedPhoneAttrs: atributos personalizados do contato consultados, nessa
// ordem, quando ele não tem phone_number (contatos criados pelo WAHA).
var defaultEmbedPhoneAttrs = []string{"waha_whatsapp_jid", "waha_whatsapp_chat_id", "waha_whatsapp_lid"}

type embedConfig struct {
	// Inboxes: id da caixa de entrada do Chatwoot -> id da sessão (número da loja).
	Inboxes map[string]string `json:"inboxes"`
	// PhoneAttrs: atributos do contato com o número, em ordem de preferência.
	PhoneAttrs []string `json:"phoneAttrs"`
}

// embedSettings junta o que vem do ambiente (WACALLS_EMBED_INBOXES="12=sid,13=sid2",
// WACALLS_EMBED_PHONE_ATTRS="a,b") com o que o administrador salvou no painel
// (tabela settings; vence o ambiente).
type embedSettings struct {
	mu    sync.RWMutex
	saved embedConfig
	db    *sql.DB
}

const embedSettingsKey = "embed_chatwoot"

func newEmbedSettings(ctx context.Context, db *sql.DB) (*embedSettings, error) {
	e := &embedSettings{db: db}
	if db == nil {
		return e, nil
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value JSONB NOT NULL)`); err != nil {
		return nil, err
	}
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, embedSettingsKey).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal(raw, &e.saved)
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return e, nil
}

// get devolve a configuração efetiva (ambiente + salva).
func (e *embedSettings) get() embedConfig {
	out := embedConfig{Inboxes: map[string]string{}, PhoneAttrs: defaultEmbedPhoneAttrs}
	for _, pair := range splitList(os.Getenv("WACALLS_EMBED_INBOXES")) {
		if k, v, ok := strings.Cut(pair, "="); ok && strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out.Inboxes[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if attrs := splitList(os.Getenv("WACALLS_EMBED_PHONE_ATTRS")); len(attrs) > 0 {
		out.PhoneAttrs = attrs
	}
	if e == nil {
		return out
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for k, v := range e.saved.Inboxes {
		out.Inboxes[k] = v
	}
	if len(e.saved.PhoneAttrs) > 0 {
		out.PhoneAttrs = e.saved.PhoneAttrs
	}
	return out
}

func (e *embedSettings) set(ctx context.Context, c embedConfig) error {
	clean := embedConfig{Inboxes: map[string]string{}}
	for k, v := range c.Inboxes {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if _, err := strconv.Atoi(k); err != nil || v == "" {
			continue // caixa de entrada é número; sessão vazia = remover
		}
		clean.Inboxes[k] = v
	}
	for _, a := range c.PhoneAttrs {
		if a = strings.TrimSpace(a); a != "" {
			clean.PhoneAttrs = append(clean.PhoneAttrs, a)
		}
	}
	if e.db != nil {
		raw, _ := json.Marshal(clean)
		if _, err := e.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ($1,$2)
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, embedSettingsKey, raw); err != nil {
			return err
		}
	}
	e.mu.Lock()
	e.saved = clean
	e.mu.Unlock()
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// embedOrigins são as origens (ex.: https://chat.sualoja.com.br) que podem
// embutir /embed/chatwoot (WACALLS_EMBED_ORIGINS, separadas por vírgula). Vazio =
// ninguém embute.
func embedOrigins() []string {
	var out []string
	for _, o := range splitList(os.Getenv("WACALLS_EMBED_ORIGINS")) {
		if h := chatwootHost(o); h != "" && (strings.HasPrefix(o, "https://") || strings.HasPrefix(o, "http://")) {
			out = append(out, strings.TrimRight(o, "/"))
		}
	}
	return out
}

// frameAncestors monta o CSP: só /embed/* pode ser embutido, e só pelas origens
// configuradas; o resto do painel não pode ser embutido por ninguém.
func frameAncestors(path string) string {
	if strings.HasPrefix(path, "/embed/") {
		if o := embedOrigins(); len(o) > 0 {
			return "frame-ancestors " + strings.Join(o, " ")
		}
	}
	return "frame-ancestors 'none'"
}

// GET /api/embed/config — atendente e admin (a página embutida precisa).
func (s *server) handleGetEmbedConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.embed.get())
}

// PUT /api/embed/config {inboxes, phoneAttrs} — só chave-mestra.
func (s *server) handleSetEmbedConfig(w http.ResponseWriter, r *http.Request) {
	var c embedConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload inválido"})
		return
	}
	if err := s.embed.set(r.Context(), c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.embed.get())
}

// GET /api/sessions/{sid}/lid-phone?lid=123@lid — telefone real de um LID, pelo
// mapa da sessão (contatos do WAHA às vezes só têm o @lid). 404 se não conhecido.
func (s *server) handleLIDPhone(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid, err := types.ParseJID(strings.TrimSpace(r.URL.Query().Get("lid")))
	if err != nil || jid.Server != types.HiddenUserServer {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lid inválido (esperado 123...@lid)"})
		return
	}
	if sess.client == nil || sess.client.Store == nil || sess.client.Store.LIDs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	phone, ok := sess.resolvedPhone(jid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "número deste contato ainda não é conhecido"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"phone": phone})
}

// servedUnderChatwoot: pedido que chegou pelo caminho do domínio do Chatwoot
// (modo mesma origem): o proxy mandou X-Forwarded-Prefix e o host é um dos
// Chatwoot de WACALLS_EMBED_ORIGINS. Um StripPrefix (ex.: Traefik) no domínio
// próprio do AstraCalls não entra aqui.
func servedUnderChatwoot(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Forwarded-Prefix")) == "" {
		return false
	}
	host := strings.ToLower(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.ToLower(r.Host)
	}
	for _, o := range embedOrigins() {
		if chatwootHost(o) == host {
			return true
		}
	}
	return false
}
