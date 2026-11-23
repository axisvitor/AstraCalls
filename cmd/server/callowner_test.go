package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Um atendente não opera a chamada de outro da mesma linha: nem assumir o áudio
// (ws/webrtc), nem espera, transferir, desligar, recusar ou pickup. Atende/recusa
// chamada que ainda toca sem dono e assume só a transferida para ele.
func TestAgentCallOwnership(t *testing.T) {
	d, _ := newTestAgents(t) // "maria" com s1
	bob, _ := d.create(context.Background(), "bob", "Bob", "senha-forte-2", []string{"s1"})
	maria := loginToken(t, d)
	bobTok, _, _, _ := d.login("bob", "senha-forte-2", "198.51.100.1")
	mariaAgent, _ := d.fromToken(maria)
	mariaOwner := agentOwnerPrefix + mariaAgent.ID

	b := NewBroker()
	srv := &server{agents: d, broker: b}
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "dela", Owner: &mariaOwner, Direction: "inbound", Status: StatusConnected})
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "tocando", Direction: "inbound", Status: StatusRinging})
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "transf", Owner: &mariaOwner, Direction: "inbound", Status: StatusConnected})
	b.offerTransfer("transf", "browser-bob")
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "saindo", Direction: "outbound", Status: StatusRinging})

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withAuth(srv.withCallOwnership(ok), "mestra", "", nil, d.fromToken)
	do := func(tok, browser, method, path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-API-Key", tok)
		req.Header.Set("X-Client-Id", browser)
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	cases := []struct {
		who, browser, method, path string
		want                       int
	}{
		// chamada da Maria: o Bob não mexe, nem forjando o X-Client-Id dela
		{bobTok, mariaOwner, "GET", "/api/sessions/s1/calls/dela/ws", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/webrtc", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/hold", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/resume", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/transfer", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/mute", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/pickup", 403},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/reject", 403},
		{bobTok, "b", "DELETE", "/api/sessions/s1/calls/dela", 403},
		{maria, "m", "GET", "/api/sessions/s1/calls/dela/ws", 200},
		{maria, "m", "DELETE", "/api/sessions/s1/calls/dela", 200},
		{"mestra", "admin", "GET", "/api/sessions/s1/calls/dela/ws", 200},
		// tocando sem dono: qualquer um atende/recusa, mas não assume o áudio antes
		{bobTok, "b", "POST", "/api/sessions/s1/calls/tocando/accept", 200},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/tocando/reject", 200},
		{bobTok, "b", "POST", "/api/sessions/s1/calls/tocando/hold", 403},
		{bobTok, "b", "GET", "/api/sessions/s1/calls/tocando/ws", 403},
		// transferida para o navegador do Bob: só ele assume
		{maria, "m", "POST", "/api/sessions/s1/calls/transf/pickup", 403},
		{bobTok, "browser-bob", "POST", "/api/sessions/s1/calls/transf/pickup", 200},
		{bobTok, "browser-bob", "GET", "/api/sessions/s1/calls/transf/ws", 403},
		// id de chamada de outra linha na URL da linha dele
		{bobTok, "b", "POST", "/api/sessions/s2/calls/dela/hold", 403},
		// atender a que a Maria já pegou: 409, como o doAccept (o painel silencia)
		{bobTok, "b", "POST", "/api/sessions/s1/calls/dela/accept", 409},
		// saída sem dono gravado ainda (não é transferência): ninguém assume
		{bobTok, "b", "POST", "/api/sessions/s1/calls/saindo/pickup", 403},
	}
	for _, c := range cases {
		if got := do(c.who, c.browser, c.method, c.path); got != c.want {
			t.Errorf("%s %s (cliente %s) = %d, quer %d", c.method, c.path, c.browser, got, c.want)
		}
	}
	_ = bob
}

// O dono gravado é o atendente do token, não o X-Client-Id que o navegador manda.
func TestCallOwnerIDFromToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/sessions/s1/calls", nil)
	req.Header.Set("X-Client-Id", "qualquer")
	if callOwnerID(req) != "qualquer" {
		t.Fatal("admin deveria continuar dono pelo clientId")
	}
	req = withAgentCtx(req, &agent{ID: "A1"})
	if callOwnerID(req) != "agent:A1" {
		t.Fatalf("dono do atendente = %q", callOwnerID(req))
	}
}
