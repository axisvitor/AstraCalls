package main

import (
	"context"
	"log/slog"
	"testing"
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

// Com WACALLS_RING_TIMEOUT_REJECT desligado (padrão no modo só ligações) o toque
// expirado só sai do painel: nada é enviado ao WhatsApp (o cm nil entraria em
// pânico se RejectCall fosse chamado) e a chamada vira perdida.
func TestRingTimeoutWithoutReject(t *testing.T) {
	prev := ringTimeoutReject
	t.Cleanup(func() { ringTimeoutReject = prev })

	newSess := func() (*Session, *Broker) {
		b := NewBroker()
		mgr := &SessionManager{broker: b, log: slog.Default(), appCtx: context.Background(), sessions: map[string]*Session{}}
		s := &Session{id: "s1", mgr: mgr, log: slog.Default(), reg: newCallRegistry()}
		s.reg.add("c1", &activeCall{spies: newSpyHub()})
		b.upsertCall(CallRecord{SessionID: "s1", CallID: "c1", Direction: "inbound", Status: StatusRinging})
		return s, b
	}

	ringTimeoutReject = false
	s, b := newSess()
	if panics(func() { s.expireRingingCall("c1") }) {
		t.Fatal("sem rejeitar, mas tentou recusar no WhatsApp")
	}
	if _, ok := s.reg.get("c1"); ok {
		t.Fatal("chamada continuou no registro")
	}
	if rows := b.historyRows("s1", 1); len(rows) != 1 || !rows[0].Missed || rows[0].EndReason != "timeout" {
		t.Fatalf("histórico = %+v", rows)
	}

	ringTimeoutReject = true
	s, _ = newSess()
	if !panics(func() { s.expireRingingCall("c1") }) {
		t.Fatal("com reject ligado deveria recusar no WhatsApp (teste vazio)")
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
