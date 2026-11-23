package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/call"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// Chamada perdida = recebida, nunca conectada, sem ninguém atendendo no painel e
// não atendida em outro aparelho. OnEnded (persistência) recebe cada uma 1 vez.
func TestMissedCallFlag(t *testing.T) {
	owner := "browser-1"
	cases := []struct {
		name   string
		rec    CallRecord
		reason string
		missed bool
	}{
		{"recebida que tocou e acabou", CallRecord{Direction: "inbound", Status: StatusRinging}, "timeout", true},
		{"quem ligou desistiu", CallRecord{Direction: "inbound", Status: StatusRinging}, "user_ended", true},
		{"recusada no painel", CallRecord{Direction: "inbound", Status: StatusRinging}, "declined", true},
		{"atendida no celular", CallRecord{Direction: "inbound", Status: StatusRinging}, "accepted_elsewhere", false},
		{"atendida no painel", CallRecord{Direction: "inbound", Status: StatusConnected, Owner: &owner}, "user_ended", false},
		{"atendida, falhou ao conectar", CallRecord{Direction: "inbound", Status: StatusRinging, Owner: &owner}, "failed", false},
		{"feita pelo painel", CallRecord{Direction: "outbound", Status: StatusRinging, Owner: &owner}, "timeout", false},
	}
	for _, c := range cases {
		b := NewBroker()
		var got []CallRecord
		b.OnEnded = func(r CallRecord) { got = append(got, r) }
		c.rec.SessionID, c.rec.CallID = "s1", "c1"
		b.upsertCall(c.rec)
		b.endCall("c1", c.reason)
		b.endCall("c1", c.reason) // 2º encerramento (OnStateChange + OnEnded) é ignorado
		if len(got) != 1 {
			t.Fatalf("%s: OnEnded chamado %d vezes", c.name, len(got))
		}
		if got[0].Missed != c.missed || b.historyRows("s1", 1)[0].Missed != c.missed {
			t.Errorf("%s: missed=%v, quer %v", c.name, got[0].Missed, c.missed)
		}
	}
}

// Histórico carregado do banco aparece antes das chamadas novas.
func TestLoadHistoryOrder(t *testing.T) {
	b := NewBroker()
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "nova", Direction: "outbound"})
	b.endCall("nova", "user_ended")
	b.loadHistory([]CallRecord{{SessionID: "s1", CallID: "antiga"}})
	rows := b.historyRows("s1", 10)
	if len(rows) != 2 || rows[0].CallID != "nova" || rows[1].CallID != "antiga" {
		t.Fatalf("ordem do histórico = %+v", rows)
	}
}

// recSock é um core.VoipSocket que só registra o que sairia para o WhatsApp.
type recSock struct {
	mu   sync.Mutex
	tags []string
}

func (f *recSock) add(n waBinary.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tag := n.Tag
	if ch, ok := n.Content.([]waBinary.Node); ok && len(ch) > 0 {
		tag += "/" + ch[0].Tag
	}
	f.tags = append(f.tags, tag)
}
func (f *recSock) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tags...)
}
func (f *recSock) OwnPN() types.JID  { return types.NewJID("5511900000000", types.DefaultUserServer) }
func (f *recSock) OwnLID() types.JID { return types.NewJID("999", types.HiddenUserServer) }
func (f *recSock) AccountDeviceIdentityNode() (waBinary.Node, bool) {
	return waBinary.Node{}, false
}
func (f *recSock) SendNode(_ context.Context, n waBinary.Node) error { f.add(n); return nil }
func (f *recSock) Query(_ context.Context, n waBinary.Node) (*waBinary.Node, error) {
	f.add(n)
	return &waBinary.Node{}, nil
}
func (f *recSock) GetUSyncDevices(context.Context, []types.JID) ([]types.JID, error) { return nil, nil }
func (f *recSock) AssertSessions(context.Context, []types.JID, bool) error           { return nil }
func (f *recSock) CreateParticipantNodes(context.Context, []types.JID, []byte, waBinary.Attrs) ([]waBinary.Node, bool, error) {
	return nil, false, nil
}
func (f *recSock) DecryptCallKey(context.Context, types.JID, *waBinary.Node) ([]byte, error) {
	return nil, nil
}
func (f *recSock) GetTCToken(context.Context, types.JID) ([]byte, error)     { return nil, nil }
func (f *recSock) ResolveLIDForPN(_ context.Context, pn types.JID) types.JID { return pn }

// ringingSession monta uma sessão com uma chamada RECEBIDA tocando (offer real
// processado por um CallManager com socket falso).
func ringingSession(t *testing.T) (*Session, *Broker, *recSock) {
	t.Helper()
	b := NewBroker()
	mgr := &SessionManager{broker: b, log: slog.Default(), appCtx: context.Background(), sessions: map[string]*Session{}}
	s := &Session{id: "s1", mgr: mgr, log: slog.Default(), reg: newCallRegistry()}
	sock := &recSock{}
	cm := call.NewCallManager(sock, slog.Default())
	s.reg.add("c1", &activeCall{cm: cm, spies: newSpyHub()})
	peer := types.NewJID("5511988887777", types.DefaultUserServer)
	cm.HandleCallOffer(context.Background(), wrapCall(peer, &waBinary.Node{
		Tag: "offer", Attrs: waBinary.Attrs{"call-id": "c1", "call-creator": peer},
	}), peer, "")
	b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Direction: "inbound", Status: StatusRinging})
	return s, b, sock
}

func waitTags(sock *recSock, want string) bool {
	for i := 0; i < 50; i++ {
		for _, tg := range sock.sent() {
			if strings.Contains(tg, want) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Com a recusa automática desligada (padrão no modo só ligações) o toque expirado
// só sai do painel: nenhum <reject> vai ao WhatsApp (o celular segue tocando), a
// chamada é liberada e vira perdida no histórico. Ligada, o <reject> sai.
func TestRingTimeoutWithoutReject(t *testing.T) {
	prev := autoReject
	t.Cleanup(func() { autoReject = prev })

	autoReject = false
	s, b, sock := ringingSession(t)
	s.expireRingingCall("c1")
	if waitTags(sock, "reject") || waitTags(sock, "terminate") {
		t.Fatalf("sem recusa automática, mas enviou %v", sock.sent())
	}
	if _, ok := s.reg.get("c1"); ok {
		t.Fatal("chamada continuou no registro")
	}
	if rows := b.historyRows("s1", 1); len(rows) != 1 || !rows[0].Missed || rows[0].EndReason != "timeout" {
		t.Fatalf("histórico = %+v", rows)
	}

	autoReject = true
	s, _, sock = ringingSession(t)
	s.expireRingingCall("c1")
	if !waitTags(sock, "reject") {
		t.Fatalf("com recusa automática deveria enviar <reject>; enviou %v", sock.sent())
	}
}

func TestEnvBoolDefault(t *testing.T) {
	t.Setenv("X_BOOL", "")
	if !envBoolDefault("X_BOOL", true) || envBoolDefault("X_BOOL", false) {
		t.Fatal("vazio deveria usar o padrão")
	}
	t.Setenv("X_BOOL", "false")
	if envBoolDefault("X_BOOL", true) {
		t.Fatal("false explícito ignorado")
	}
	t.Setenv("X_BOOL", "1")
	if !envBoolDefault("X_BOOL", false) {
		t.Fatal("1 explícito ignorado")
	}
}
