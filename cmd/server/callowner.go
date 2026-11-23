package main

import (
	"net/http"
	"strings"
)

// Dono de chamada. Para o ATENDENTE o dono é "agent:<id>", vindo do token (não
// do X-Client-Id, que o próprio navegador escolhe); para admin/widget continua
// sendo o clientId do navegador, como antes.
const agentOwnerPrefix = "agent:"

func callOwnerID(r *http.Request) string {
	if a := agentFrom(r); a != nil {
		return agentOwnerPrefix + a.ID
	}
	return clientID(r)
}

// ringingActions: o que qualquer atendente da linha faz numa chamada RECEBIDA que
// ainda toca sem dono (atender, recusar, silenciar o toque no próprio painel).
var ringingActions = map[string]bool{"accept": true, "reject": true, "silence": true}

// withCallOwnership é o guarda ÚNICO das ações de chamada do atendente
// (/api/sessions/{sid}/calls/{id}[/ação]): ele só opera a própria chamada, uma que
// ainda toca sem dono, ou uma transferida para ele (pickup). Admin e widget passam
// direto. Roda depois do withAuth (que põe o atendente no contexto).
func (s *server) withCallOwnership(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := agentFrom(r)
		if a == nil {
			h.ServeHTTP(w, r)
			return
		}
		rest, _ := strings.CutPrefix(r.URL.Path, "/api/sessions/")
		segs := strings.Split(rest, "/")
		if len(segs) < 3 || segs[1] != "calls" {
			h.ServeHTTP(w, r)
			return
		}
		action := ""
		if len(segs) > 3 {
			action = segs[3]
		}
		rec, ok := s.broker.getCall(segs[2])
		if !ok {
			h.ServeHTTP(w, r) // chamada desconhecida: o handler responde 404
			return
		}
		me := callOwnerID(r)
		if !agentMayOperate(rec, segs[0], action, me, clientID(r)) {
			// atender uma que outro já pegou: mesmo 409 do doAccept (o painel silencia)
			if action == "accept" && rec.Owner != nil && rec.SessionID == segs[0] {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "claimed by another client"})
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "call_owned_by_another"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

func agentMayOperate(rec *CallRecord, sid, action, me, browser string) bool {
	switch {
	case rec.SessionID != sid:
		return false
	case rec.Owner != nil:
		return *rec.Owner == me
	case rec.Status == StatusRinging && rec.Direction == "inbound":
		return ringingActions[action]
	case rec.Transferred: // oferecida por transferência: só o alvo assume
		return action == "pickup" && (rec.TransferTo == "" || rec.TransferTo == browser)
	}
	return false // sem dono e sem oferta (ex.: saída ainda sem dono gravado)
}
