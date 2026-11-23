package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// setCallsOnly liga/desliga o modo só ligações durante um teste.
func setCallsOnly(t *testing.T, on bool) {
	t.Helper()
	prev := callsOnly
	callsOnly = on
	t.Cleanup(func() { callsOnly = prev })
}

// panics diz se fn entrou em pânico (aqui: tocou no cliente do WhatsApp, que é nil).
func panics(fn func()) (p bool) {
	defer func() { p = recover() != nil }()
	fn()
	return false
}

// Em modo só ligações as rotas de mensagem, disparo e Chatwoot recusam com 403 e
// erro claro, passando pelo roteador real.
func TestCallsOnlyRefusesMessageRoutes(t *testing.T) {
	setCallsOnly(t, true)
	h := (&server{}).routes()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/sessions/s1/messages/text"},
		{"POST", "/api/sessions/s1/messages/seen"},
		{"POST", "/api/sessions/s1/blast"},
		{"POST", "/api/sessions/s1/broadcast"},
		{"POST", "/api/sessions/s1/schedule"},
		{"POST", "/api/sessions/s1/presence"},
		{"POST", "/api/sessions/s1/status/text"},
		{"POST", "/api/sessions/s1/chatwoot"},
		{"GET", "/api/chatwoot/resolve"},
		{"POST", "/api/mcp"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, quer 403", c.method, c.path, rec.Code)
		}
	}
	// rotas de chamada seguem liberadas
	for _, p := range []string{"/api/sessions/s1/calls", "/api/sessions/s1/calls/abc/accept", "/api/sessions/s1/history", "/api/config", "/api/events", "/api/sessions"} {
		if callsOnlyBlocked(p) {
			t.Errorf("%s não deveria ser bloqueada", p)
		}
	}
}

// Fora do modo só ligações nada muda: o filtro nem é instalado.
func TestCallsOnlyOffKeepsRoutes(t *testing.T) {
	setCallsOnly(t, false)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	withCallsOnly(ok).ServeHTTP(rec, httptest.NewRequest("POST", "/api/sessions/s1/messages/text", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("modo normal bloqueou rota de mensagem: %d", rec.Code)
	}
}

// Nenhuma presença "available" é enviada em modo só ligações. O cliente do
// WhatsApp é nil: se o código chegasse a usá-lo, entraria em pânico. Com o modo
// desligado o mesmo teste entra em pânico, prova de que o caminho é exercitado.
func TestCallsOnlyNoPresence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s := &Session{}

	setCallsOnly(t, true)
	if panics(func() { s.keepPresenceActive(ctx) }) {
		t.Fatal("modo só ligações tentou enviar presença")
	}
	callsOnly = false
	if !panics(func() { s.keepPresenceActive(ctx) }) {
		t.Fatal("modo normal deveria tentar enviar presença (teste vazio)")
	}
}

// Nenhum recibo de leitura em modo só ligações, mesmo com read_messages salvo.
func TestCallsOnlyNoReadReceipt(t *testing.T) {
	setCallsOnly(t, true)
	s := &Session{chatwoot: ChatwootConfig{ReadMessages: true}}
	evt := &events.Message{Info: types.MessageInfo{
		MessageSource: types.MessageSource{Chat: types.NewJID("5511999999999", types.DefaultUserServer)},
		ID:            "X1",
	}}
	if panics(func() { s.maybeMarkRead(context.Background(), evt) }) {
		t.Fatal("modo só ligações tentou marcar como lida")
	}
	if s.getChatwoot().valid() || s.getChatwoot().ReadMessages {
		t.Fatal("config do Chatwoot deveria ser ignorada em modo só ligações")
	}
}

// Conteúdo de mensagem não vai para o banco em modo só ligações (db nil: gravar
// entraria em pânico).
func TestCallsOnlyDoesNotStoreMessages(t *testing.T) {
	st := &sessionStore{}
	setCallsOnly(t, true)
	if panics(func() { _ = st.saveMessage(context.Background(), "s1", storedMessage{}) }) {
		t.Fatal("modo só ligações gravou mensagem")
	}
	callsOnly = false
	if !panics(func() { _ = st.saveMessage(context.Background(), "s1", storedMessage{}) }) {
		t.Fatal("modo normal deveria gravar (teste vazio)")
	}
}

func TestSIPListenAddr(t *testing.T) {
	cases := []struct {
		calls  bool
		env    string
		addr   string
		listen bool
	}{
		{false, "", "0.0.0.0:5060", true},
		{false, "off", "", false},
		{true, "", "", false},
		{true, "127.0.0.1:5070", "127.0.0.1:5070", true},
		{true, "off", "", false},
	}
	for _, c := range cases {
		setCallsOnly(t, c.calls)
		t.Setenv("WACALLS_SIP_ADDR", c.env)
		addr, on := sipListenAddr()
		if addr != c.addr || on != c.listen {
			t.Errorf("callsOnly=%v env=%q: (%q,%v), quer (%q,%v)", c.calls, c.env, addr, on, c.addr, c.listen)
		}
	}
}
