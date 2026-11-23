package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
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
		if callsOnlyBlocked("POST", p) {
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

// Envio de mensagem: o ponto único (Session.send) e o sendResolvingLID recusam em
// modo só ligações sem tocar no cliente do WhatsApp (nil aqui).
func TestCallsOnlyBlocksSends(t *testing.T) {
	s := &Session{}
	jid := types.NewJID("5511999999999", types.DefaultUserServer)
	setCallsOnly(t, true)
	var err error
	if panics(func() { _, err = s.send(context.Background(), jid, nil) }) || !errors.Is(err, errCallsOnlySend) {
		t.Fatalf("send: err=%v", err)
	}
	if panics(func() { _, err = s.sendResolvingLID(context.Background(), jid, nil) }) || !errors.Is(err, errCallsOnlySend) {
		t.Fatalf("sendResolvingLID: err=%v", err)
	}
	callsOnly = false
	panics(func() { _, err = s.send(context.Background(), jid, nil) })
	if errors.Is(err, errCallsOnlySend) {
		t.Fatal("modo normal recusou o envio")
	}
	// rotas de grupo que mandam mensagem também ficam de fora
	setCallsOnly(t, true)
	for _, p := range []string{"/api/sessions/s1/groups/123@g.us/status", "/api/sessions/s1/groups/123@g.us/history/share",
		"/api/sessions/s1/disappearing", "/api/sessions/s1/chats/5511@s.whatsapp.net/disappearing"} {
		if !callsOnlyBlocked("POST", p) {
			t.Errorf("%s deveria ser bloqueada", p)
		}
	}
	if callsOnlyBlocked("GET", "/api/sessions/s1/groups/123@g.us") {
		t.Error("consultar grupo não manda mensagem")
	}
}

// No limite de chamadas, sem recusa automática a oferta é só ignorada (nada vai
// ao WhatsApp); com ela, sai o <reject> (rejectOffer, visto pelo log).
func TestCapacityWithoutAutoReject(t *testing.T) {
	prev := autoReject
	t.Cleanup(func() { autoReject = prev })
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, nil))
	mgr := &SessionManager{broker: NewBroker(), log: log, maxCalls: 1, sessions: map[string]*Session{}}
	s := &Session{id: "s1", mgr: mgr, log: log, reg: newCallRegistry()}
	s.reg.add("ocupada", &activeCall{spies: newSpyHub()})
	peer := types.NewJID("5511988887777", types.DefaultUserServer)
	evt := &events.CallOffer{BasicCallMeta: types.BasicCallMeta{From: peer, CallID: "c2", CallCreator: peer},
		Data: &waBinary.Node{Tag: "offer", Attrs: waBinary.Attrs{"call-id": "c2", "call-creator": peer}}}

	autoReject = false
	panics(func() { s.onIncomingOffer(context.Background(), evt) })
	if strings.Contains(logBuf.String(), "inbound call rejected") || !strings.Contains(logBuf.String(), "ignorada") {
		t.Fatalf("sem recusa automática: log = %s", logBuf.String())
	}
	if s.reg.count() != 1 {
		t.Fatal("chamada acima do limite entrou no registro")
	}
	logBuf.Reset()
	autoReject = true
	panics(func() { s.onIncomingOffer(context.Background(), evt) })
	if !strings.Contains(logBuf.String(), "inbound call rejected") {
		t.Fatalf("com recusa automática deveria recusar: log = %s", logBuf.String())
	}
}

// Escritas que sincronizam com o celular ficam bloqueadas; leituras seguem.
func TestCallsOnlyBlocksAccountWrites(t *testing.T) {
	setCallsOnly(t, true)
	h := (&server{}).routes()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/sessions/s1/contacts/5511@s.whatsapp.net/block"},
		{"POST", "/api/sessions/s1/contacts/5511@s.whatsapp.net/unblock"},
		{"POST", "/api/sessions/s1/groups"},
		{"POST", "/api/sessions/s1/groups/1@g.us/leave"},
		{"PUT", "/api/sessions/s1/groups/1@g.us/subject"},
		{"POST", "/api/sessions/s1/groups/1@g.us/participants/remove"},
		{"PUT", "/api/sessions/s1/privacy"},
		{"PUT", "/api/sessions/s1/profile/status"},
		{"POST", "/api/sessions/s1/channels/1@newsletter/follow"},
		{"POST", "/api/sessions/s1/communities"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, quer 403", c.method, c.path, rec.Code)
		}
	}
	for _, p := range []string{"/api/sessions/s1/groups", "/api/sessions/s1/contacts/5511@s.whatsapp.net", "/api/sessions/s1/privacy", "/api/sessions/s1/profile"} {
		if callsOnlyBlocked("GET", p) {
			t.Errorf("leitura %s não deveria ser bloqueada", p)
		}
	}
}

// Webhook "message" (e demais com conteúdo) não sai em modo só ligações; os de
// chamada/sessão continuam. Fora do modo, tudo sai.
func TestCallsOnlyWebhookNoMessage(t *testing.T) {
	sink := &hookSink{}
	srv := httptest.NewServer(sink.handler())
	defer srv.Close()
	s := &Session{id: "w"}
	_ = s.updateWebhooks(context.Background(), func([]WebhookTarget) ([]WebhookTarget, error) {
		return []WebhookTarget{{ID: webhookDefaultID, URL: srv.URL}}, nil
	})
	setCallsOnly(t, true)
	s.dispatchWebhook("message", map[string]any{"text": "segredo"})
	s.dispatchWebhook("reaction", map[string]any{})
	s.dispatchWebhook("session_status", map[string]any{"state": "open"})
	callsOnly = false
	s.dispatchWebhook("message", map[string]any{"text": "fora do modo"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(sink.got()) < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond) // dá tempo de chegar o que não deveria
	got := sink.got()
	if len(got) != 2 || got[0] == "reaction" || got[1] == "reaction" {
		t.Fatalf("eventos entregues = %v, quer só session_status (modo) e message (fora do modo)", got)
	}
	for _, e := range got {
		if e != "session_status" && e != "message" {
			t.Fatalf("evento inesperado %s", e)
		}
	}
}
