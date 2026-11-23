package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Sem chave, "%2Fchatwoot%2Fwebhook" no fim de uma rota com curinga chegava ao
// handler (o withAuth olhava o caminho decodificado; o roteador, o escapado).
func TestAuthEncodedSlashBypass(t *testing.T) {
	t.Setenv("WACALLS_API_KEY", "mestra")
	setCallsOnly(t, false)
	h := (&server{}).routes() // handler alcançado = pânico (sem sessões)
	for _, p := range []string{
		"/api/sessions/s1/contacts/123@s.whatsapp.net%2Fchatwoot%2Fwebhook",
		"/api/sessions/s1/groups/1%2fchatwoot%2fwebhook",
		"/api/sessions/s1/chatwoot%2Fwebhook",
	} {
		rec := httptest.NewRecorder()
		if panics(func() { h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil)) }) {
			t.Fatalf("%s chegou ao handler sem chave", p)
		}
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, quer 400/401", p, rec.Code)
		}
	}
	// a rota real do webhook continua isenta (chega ao handler: sem sessões = pânico)
	if !panics(func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/sessions/s1/chatwoot/webhook", nil))
	}) {
		t.Fatal("webhook do Chatwoot deixou de ser isento")
	}
}

// A chave de widget só abre /calls de verdade, não um trecho "/calls" em outro lugar.
func TestWidgetCallsSegment(t *testing.T) {
	if widgetAllowed(httptest.NewRequest("GET", "/api/sessions/s1/contacts/123/calls", nil)) {
		t.Fatal("widget abriu rota de contatos por conter /calls")
	}
	if !widgetAllowed(httptest.NewRequest("POST", "/api/sessions/s1/calls/x/accept", nil)) {
		t.Fatal("widget perdeu as rotas de chamada")
	}
}
