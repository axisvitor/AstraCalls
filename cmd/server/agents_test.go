package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func newTestAgents(t *testing.T) (*agentDirectory, agent) {
	t.Helper()
	t.Setenv("WACALLS_API_KEY", "mestra")
	d, _ := newAgentDirectory(context.Background(), nil)
	a, err := d.create(context.Background(), "Maria", "Maria Vendas", "senha-forte-1", []string{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	return d, a
}

func loginToken(t *testing.T, d *agentDirectory) string {
	t.Helper()
	tok, _, _, err := d.login("maria", "senha-forte-1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return tok
}

// Senha errada repetida trava o usuário (mesmo com a senha certa depois), e
// destrava sozinho após agentLockFor.
func TestAgentLoginRateLimited(t *testing.T) {
	d, _ := newTestAgents(t)
	now := time.Now()
	d.now = func() time.Time { return now }
	for i := 0; i < agentMaxFails; i++ {
		if _, _, _, err := d.login("maria", "errada"); !errors.Is(err, errLoginBad) {
			t.Fatalf("tentativa %d: err=%v, quer errLoginBad", i, err)
		}
	}
	if _, _, _, err := d.login("maria", "senha-forte-1"); !errors.Is(err, errLoginLocked) {
		t.Fatalf("depois de %d erros: err=%v, quer errLoginLocked", agentMaxFails, err)
	}
	now = now.Add(agentLockFor + time.Second)
	if _, _, _, err := d.login("Maria", "senha-forte-1"); err != nil {
		t.Fatalf("após o bloqueio: %v", err)
	}
	// usuário inexistente responde igual a senha errada (não revela quem existe)
	if _, _, _, err := d.login("ninguem", "x"); !errors.Is(err, errLoginBad) {
		t.Fatalf("usuário inexistente: %v", err)
	}
}

// O login HTTP fica fora da chave e devolve 429 quando travado.
func TestAgentLoginHTTP(t *testing.T) {
	d, _ := newTestAgents(t)
	srv := &server{agents: d}
	h := withAuth(http.HandlerFunc(srv.handleAgentLogin), "mestra", "", nil, d.fromToken)
	post := func(pw string) int {
		body, _ := json.Marshal(map[string]string{"username": "maria", "password": pw})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/agent/login", bytes.NewReader(body))
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := post("senha-forte-1"); c != http.StatusOK {
		t.Fatalf("login certo = %d", c)
	}
	for i := 0; i < agentMaxFails; i++ {
		if c := post("errada"); c != http.StatusUnauthorized {
			t.Fatalf("senha errada = %d, quer 401", c)
		}
	}
	if c := post("senha-forte-1"); c != http.StatusTooManyRequests {
		t.Fatalf("travado = %d, quer 429", c)
	}
}

// O atendente só alcança a superfície de chamadas das sessões dele: rotas de
// administrador e sessões de outros dão 403.
func TestAgentCannotReachAdminRoutes(t *testing.T) {
	d, _ := newTestAgents(t)
	tok := loginToken(t, d)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agentFrom(r) == nil {
			t.Errorf("%s %s: atendente não chegou no contexto", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})
	h := withAuth(ok, "mestra", "", nil, d.fromToken)
	do := func(method, path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-API-Key", tok)
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	allowed := []struct{ m, p string }{
		{"GET", "/api/config"},
		{"GET", "/api/events"},
		{"GET", "/api/sessions"},
		{"GET", "/api/sessions/s1/history"},
		{"POST", "/api/sessions/s1/calls"},
		{"POST", "/api/sessions/s1/calls/c1/accept"},
		{"POST", "/api/sessions/s1/calls/c1/hold"},
		{"POST", "/api/sessions/s1/calls/c1/transfer"},
		{"POST", "/api/sessions/s1/calls/c1/pickup"},
		{"GET", "/api/sessions/s1/calls/c1/ws"},
		{"DELETE", "/api/sessions/s1/calls/c1"},
	}
	for _, c := range allowed {
		if code := do(c.m, c.p); code != http.StatusOK {
			t.Errorf("%s %s = %d, quer 200", c.m, c.p, code)
		}
	}
	denied := []struct{ m, p string }{
		{"GET", "/api/agents"},
		{"POST", "/api/agents"},
		{"POST", "/api/sessions"},
		{"DELETE", "/api/sessions/s1"},
		{"POST", "/api/sessions/s1/pair"},
		{"POST", "/api/sessions/s1/logout"},
		{"POST", "/api/sessions/s1/webhook"},
		{"POST", "/api/sessions/s1/messages/text"},
		{"GET", "/api/sessions/s1/sip"},
		{"POST", "/api/sessions/s1/calls/fake"},
		{"POST", "/api/sessions/s1/calls/group"},
		{"POST", "/api/sessions/s1/calls/c1/spy"},
		{"GET", "/api/widget-key"},
		// sessão que não é dele
		{"POST", "/api/sessions/s2/calls"},
		{"GET", "/api/sessions/s2/history"},
		{"POST", "/api/sessions/s2/calls/c1/accept"},
	}
	for _, c := range denied {
		if code := do(c.m, c.p); code != http.StatusForbidden {
			t.Errorf("%s %s = %d, quer 403", c.m, c.p, code)
		}
	}
}

// Token deixa de valer ao trocar a senha, ao apagar o atendente e ao expirar.
func TestAgentTokenRevocation(t *testing.T) {
	d, a := newTestAgents(t)
	tok := loginToken(t, d)
	if _, ok := d.fromToken(tok); !ok {
		t.Fatal("token recém-emitido inválido")
	}
	pw := "outra-senha-2"
	if _, err := d.update(context.Background(), a.ID, nil, &pw, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.fromToken(tok); ok {
		t.Fatal("token continuou valendo após trocar a senha")
	}
	tok2, _, _, _ := d.login("maria", pw)
	d.now = func() time.Time { return time.Now().Add(agentTokenTTL + time.Minute) }
	if _, ok := d.fromToken(tok2); ok {
		t.Fatal("token expirado aceito")
	}
	d.now = time.Now
	_ = d.remove(context.Background(), a.ID)
	if _, ok := d.fromToken(tok2); ok {
		t.Fatal("token de atendente apagado aceito")
	}
	// token assinado com outra chave-mestra não vale
	t.Setenv("WACALLS_API_KEY", "outra")
	if _, ok := d.fromToken(tok); ok {
		t.Fatal("token com assinatura de outra chave aceito")
	}
}

// Lista de sessões e eventos SSE: o atendente só vê as sessões dele, sem
// credenciais SIP e sem QR de pareamento.
func TestAgentSessionVisibility(t *testing.T) {
	a := &agent{Name: "Maria", Sessions: []string{"s1"}}
	all := []SessionInfo{{ID: "s1", SIPPass: "segredo"}, {ID: "s2"}}
	got := sessionsForAgent(a, all)
	if len(got) != 1 || got[0].ID != "s1" || got[0].SIPPass != "" {
		t.Fatalf("sessionsForAgent = %+v", got)
	}
	if len(sessionsForAgent(nil, all)) != 2 {
		t.Fatal("admin deveria ver todas as sessões")
	}

	b := NewBroker()
	sub := b.subscribeFiltered("maria", 0, "", nil, map[string]bool{"s1": true})
	admin := b.subscribe("admin", 0)
	b.upsertCall(CallRecord{SessionID: "s2", CallID: "c2", Direction: "inbound", Status: StatusRinging})
	b.emitIncoming("s2", "c2", "5511", "5511", "", false)
	b.emitAuthState("s1", AuthSnapshot{State: "qr", QR: "QR-SECRETO"})
	b.emitSessionQR("s1", "QR-SECRETO")
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Direction: "inbound", Status: StatusRinging})
	b.emitIncoming("s1", "c1", "5511", "5511", "", false)

	types := map[string]int{}
	for len(sub.ch) > 0 {
		raw := <-sub.ch
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		typ, _ := ev["type"].(string)
		types[typ]++
		if bytes.Contains(raw, []byte(`"s2"`)) || bytes.Contains(raw, []byte("QR-SECRETO")) {
			t.Errorf("atendente recebeu dado de outra sessão ou QR: %s", raw)
		}
		if typ == "call-list" {
			for _, c := range ev["calls"].([]any) {
				if c.(map[string]any)["sessionId"] != "s1" {
					t.Errorf("call-list do atendente com chamada de outra sessão: %s", raw)
				}
			}
		}
	}
	if types["incoming"] != 1 || types["auth-state"] != 1 || types["call-list"] == 0 || types["session-qr"] != 0 {
		t.Fatalf("eventos do atendente = %v", types)
	}
	adminSawS2 := false
	for len(admin.ch) > 0 {
		if bytes.Contains(<-admin.ch, []byte(`"s2"`)) {
			adminSawS2 = true
		}
	}
	if !adminSawS2 {
		t.Fatal("admin deveria ver a sessão s2 (teste vazio)")
	}
}

// O nome do atendente que atendeu/ligou vai para o histórico.
func TestAgentNameInHistory(t *testing.T) {
	b := NewBroker()
	b.AgentName = func(id string) string { return map[string]string{"A1": "Maria Vendas"}[id] }
	agentOwner := agentOwnerPrefix + "A1"
	browser := "browser-1" // clientId: escolhido pelo navegador, não identifica ninguém
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: &agentOwner, Direction: "outbound", Status: StatusConnected})
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c2", Owner: &browser, Direction: "outbound", Status: StatusConnected})
	b.endCall("c1", "user_ended")
	b.endCall("c2", "user_ended")
	rows := b.historyRows("s1", 10)
	if len(rows) != 2 || rows[1].Agent != "Maria Vendas" || rows[0].Agent != "" {
		t.Fatalf("histórico = %+v", rows)
	}
}

// O atendente renova o token com a aba aberta; o admin não usa essa rota.
func TestAgentRefresh(t *testing.T) {
	d, _ := newTestAgents(t)
	srv := &server{agents: d}
	tok := loginToken(t, d)
	h := withAuth(http.HandlerFunc(srv.handleAgentRefresh), "mestra", "", nil, d.fromToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/agent/refresh", nil)
	req.Header.Set("X-API-Key", tok)
	h.ServeHTTP(rec, req)
	var out struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out.Token == "" {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := d.fromToken(out.Token); !ok {
		t.Fatal("token renovado inválido")
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/agent/refresh", nil)
	req.Header.Set("X-API-Key", "mestra")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin no refresh = %d", rec.Code)
	}
}

// O SSE de um atendente fecha sozinho quando ele é removido (o painel reconecta,
// leva 401 e volta para o login).
func TestAgentSSEClosesOnRevoke(t *testing.T) {
	d, a := newTestAgents(t)
	prev := agentWatchEvery
	agentWatchEvery = 10 * time.Millisecond
	t.Cleanup(func() { agentWatchEvery = prev })
	srv := &server{agents: d, broker: NewBroker(), sessions: &SessionManager{sessions: map[string]*Session{}}, log: slog.Default()}
	tok := loginToken(t, d)
	h := withAuth(http.HandlerFunc(srv.handleEvents), "mestra", "", nil, d.fromToken)
	req := httptest.NewRequest("GET", "/api/events?clientId=c1", nil)
	req.Header.Set("X-API-Key", tok)
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("SSE fechou com o atendente ainda válido")
	case <-time.After(50 * time.Millisecond):
	}
	_ = d.remove(context.Background(), a.ID)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE continuou aberto depois de remover o atendente")
	}
}

// O mapa de tentativas não cresce sem fim com nomes inventados, mas quem está
// travado continua travado.
func TestAgentFailsPrune(t *testing.T) {
	d, _ := newTestAgents(t)
	now := time.Now()
	for i := 0; i < 50; i++ {
		d.fails[string(rune('a'+i%26))+strconv.Itoa(i)] = &loginFail{n: 1}
	}
	d.fails["maria"] = &loginFail{n: agentMaxFails, until: now.Add(time.Minute)}
	d.pruneFails(now)
	if len(d.fails) != 1 || d.fails["maria"] == nil {
		t.Fatalf("depois de podar: %d entradas", len(d.fails))
	}
}
