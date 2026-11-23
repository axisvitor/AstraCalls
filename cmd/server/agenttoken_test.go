package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func refreshCode(t *testing.T, d *agentDirectory, tok string) (int, string) {
	t.Helper()
	srv := &server{agents: d}
	h := withAuth(http.HandlerFunc(srv.handleAgentRefresh), "mestra", "", nil, d.fromToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/agent/refresh", nil)
	req.Header.Set("X-API-Key", tok)
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A renovação para 7 dias depois do login com senha (token roubado não vale para sempre).
func TestAgentRefreshCappedAt7Days(t *testing.T) {
	d, _ := newTestAgents(t)
	start := time.Now()
	d.now = func() time.Time { return start }
	tok := loginToken(t, d)
	// renova a cada 11 h até perto dos 7 dias: segue valendo
	h := 11
	for ; h < 7*24; h += 11 {
		d.now = func() time.Time { return start.Add(time.Duration(h) * time.Hour) }
		code, body := refreshCode(t, d, tok)
		if code != http.StatusOK {
			t.Fatalf("renovação na hora %d = %d %s", h, code, body)
		}
		var out struct{ Token string }
		_ = json.Unmarshal([]byte(body), &out)
		tok = out.Token
	}
	// h passou dos 7 dias; o último token (de 11 h atrás) ainda não expirou
	d.now = func() time.Time { return start.Add(time.Duration(h) * time.Hour) }
	if _, ok := d.fromToken(tok); !ok {
		t.Fatal("o token ainda deveria estar válido (teste mal montado)")
	}
	if code, _ := refreshCode(t, d, tok); code != http.StatusUnauthorized {
		t.Fatalf("renovação depois de 7 dias = %d, quer 401", code)
	}
}

// "Sair" invalida no servidor os tokens já emitidos; login novo funciona.
func TestAgentLogoutRevokes(t *testing.T) {
	d, a := newTestAgents(t)
	tok := loginToken(t, d)
	srv := &server{agents: d}
	h := withAuth(http.HandlerFunc(srv.handleAgentLogout), "mestra", "", nil, d.fromToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/agent/logout", nil)
	req.Header.Set("X-API-Key", tok)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if _, ok := d.fromToken(tok); ok {
		t.Fatal("token continuou valendo depois de Sair")
	}
	time.Sleep(2 * time.Millisecond)
	if _, ok := d.fromToken(loginToken(t, d)); !ok {
		t.Fatal("login depois de Sair não vale")
	}
	_ = a
}

// O SSE do atendente fecha quando o admin muda as linhas dele (o filtro é da conexão).
func TestAgentSSEClosesOnSessionsChange(t *testing.T) {
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
	go func() { h.ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	select {
	case <-done:
		t.Fatal("SSE fechou sem motivo")
	case <-time.After(50 * time.Millisecond):
	}
	none := []string{}
	if _, err := d.update(context.Background(), a.ID, nil, nil, &none); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE seguiu aberto com a linha tirada do atendente")
	}
}

// Remover o atendente derruba a ponte de áudio da chamada dele, sem encerrar a
// ligação: ela fica sem dono, para outro atendente da linha assumir.
func TestDeleteAgentDropsAudio(t *testing.T) {
	d, a := newTestAgents(t)
	b := NewBroker()
	mgr := &SessionManager{broker: b, log: slog.Default(), sessions: map[string]*Session{}}
	sess := &Session{id: "s1", mgr: mgr, log: slog.Default(), reg: newCallRegistry()}
	mgr.register(sess)
	cancelled := false
	ws := &wsBridge{cancel: func() { cancelled = true }}
	sess.reg.add("c1", &activeCall{wsBridge: ws, spies: newSpyHub()})
	owner := agentOwnerPrefix + a.ID
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Owner: &owner, Direction: "inbound", Status: StatusConnected})

	colega := b.subscribe("colega", 0) // painel de outro atendente da linha
	srv := &server{agents: d, broker: b, sessions: mgr}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/agents/"+a.ID, nil)
	req.SetPathValue("id", a.ID)
	srv.handleDeleteAgent(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	ac, ok := sess.reg.get("c1")
	if !ok || ac.wsBridge != nil || !cancelled {
		t.Fatalf("ponte de áudio não caiu (ok=%v cancel=%v)", ok, cancelled)
	}
	if c, _ := b.getCall("c1"); c == nil || c.Owner != nil || !c.Transferred {
		t.Fatal("a chamada deveria ficar sem dono, oferecida a outro atendente")
	}
	offered := false
	for len(colega.ch) > 0 {
		if strings.Contains(string(<-colega.ch), `"call-transfer-offer"`) {
			offered = true
		}
	}
	if !offered {
		t.Fatal("os colegas da linha não receberam a oferta para assumir")
	}
}
