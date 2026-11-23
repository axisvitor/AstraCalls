package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

// Modo só ligações (WACALLS_CALLS_ONLY=true): o AstraCalls fica pareado como mais
// um aparelho de um número cujas mensagens são atendidas por OUTRO sistema, e só
// faz chamadas de voz. Nesse modo ele não pode interferir no número:
//   - não envia presença "available" (o celular e o outro aparelho deixariam de
//     receber notificações) nem confirma leitura;
//   - não guarda conteúdo de mensagens no banco;
//   - não fala com o Chatwoot (a config salva é ignorada);
//   - não abre a porta SIP (a menos que WACALLS_SIP_ADDR seja setado);
//   - recusa as rotas de envio de mensagem, disparo e Chatwoot (callsOnlyBlocked).
//
// Desligado por padrão: quem não liga continua exatamente como antes.
var callsOnly = envBool("WACALLS_CALLS_ONLY")

// envBool lê uma variável de ambiente booleana ("1", "true", "yes", "on").
func envBool(key string) bool { return envBoolDefault(key, false) }

// envBoolDefault é o envBool com padrão para quando a variável não está setada.
func envBoolDefault(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on", "sim":
		return true
	case "0", "false", "no", "off", "nao", "não":
		return false
	}
	return def
}

// callsOnlyBlockedSessionRoutes são os primeiros segmentos de /api/sessions/{sid}/...
// desativados no modo só ligações: tudo que envia mensagem, presença ou disparo, e a
// integração Chatwoot.
var callsOnlyBlockedSessionRoutes = map[string]bool{
	"messages":     true, // envio, edição, leitura (seen), digitando, enquete, pix…
	"blast":        true,
	"blasts":       true,
	"broadcast":    true,
	"schedule":     true,
	"status":       true, // publicar status
	"meta-ai":      true,
	"presence":     true, // presença manual (deixaria a conta "online")
	"chatwoot":     true,
	"native-forms": true,
	"mcp-keys":     true, // MCP envia mensagens
	"disappearing": true, // mensagens temporárias (posta aviso na conversa)
}

// errCallsOnlySend é o que Session.send devolve no modo só ligações.
var errCallsOnlySend = errors.New("envio de mensagem desativado no modo só ligações (WACALLS_CALLS_ONLY)")

// callsOnlyReadOnlyRoutes: trechos de /api/sessions/{sid}/... que neste modo só
// podem ser LIDOS. Escrever neles mexe na conta e sincroniza com o celular e com
// o outro sistema (bloquear contato, sair/editar grupo, privacidade, recado,
// seguir canal, comunidades).
var callsOnlyReadOnlyRoutes = map[string]bool{
	"groups": true, "contacts": true, "communities": true, "channels": true, "profile": true, "privacy": true,
}

// callsOnlyWebhookEvents: eventos de webhook com conteúdo de mensagem, que não
// saem do servidor neste modo (os de chamada e de sessão continuam).
var callsOnlyWebhookEvents = map[string]bool{
	"message": true, "deleted": true, "reaction": true, "poll_vote": true, "event_response": true,
	"form_response": true, "view_once": true, "broadcast": true,
}

// callsOnlyBlocked diz se a rota está desativada no modo só ligações.
func callsOnlyBlocked(method, p string) bool {
	if rest, ok := strings.CutPrefix(p, "/api/sessions/"); ok {
		_, rest, _ = strings.Cut(rest, "/")
		segs := strings.Split(rest, "/")
		if method != http.MethodGet && callsOnlyReadOnlyRoutes[segs[0]] {
			return true
		}
		// status do grupo e compartilhar histórico do grupo também mandam mensagem
		if len(segs) >= 3 && segs[0] == "groups" && (segs[2] == "status" || segs[2] == "history") {
			return true
		}
		if len(segs) >= 3 && segs[0] == "chats" && segs[2] == "disappearing" {
			return true
		}
		return callsOnlyBlockedSessionRoutes[segs[0]]
	}
	return strings.HasPrefix(p, "/api/chatwoot/") || p == "/api/mcp" || p == "/mcp" || strings.HasPrefix(p, "/mcp/")
}

// withCallsOnly recusa, num ponto só, as rotas desativadas no modo só ligações.
func withCallsOnly(h http.Handler) http.Handler {
	if !callsOnly {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callsOnlyBlocked(r.Method, r.URL.Path) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "desativado no modo só ligações (WACALLS_CALLS_ONLY)",
				"code":  "calls_only",
			})
			return
		}
		h.ServeHTTP(w, r)
	})
}
