package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Só /embed/chatwoot pode ser embutido, e só pelas origens configuradas; o resto
// do painel não pode ser embutido por ninguém.
func TestEmbedFrameAncestors(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><head></head><body></body></html>"), 0o644)
	t.Setenv("WACALLS_EMBED_ORIGINS", "https://chat.loja.com.br, javascript:alert(1), https://chat2.loja.com.br/")
	h := (&server{staticDir: dir}).staticFileHandler()

	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}
	emb := get("/embed/chatwoot")
	if csp := emb.Header().Get("Content-Security-Policy"); csp != "frame-ancestors https://chat.loja.com.br https://chat2.loja.com.br" {
		t.Fatalf("CSP do embed = %q", csp)
	}
	if emb.Header().Get("X-Frame-Options") != "" || emb.Code != http.StatusOK {
		t.Fatalf("embed: code=%d xfo=%q", emb.Code, emb.Header().Get("X-Frame-Options"))
	}
	if !strings.Contains(emb.Body.String(), `__WACALLS_EMBED_ORIGINS=["https://chat.loja.com.br","https://chat2.loja.com.br"]`) {
		t.Fatalf("origens não injetadas: %s", emb.Body.String())
	}
	for _, p := range []string{"/", "/index.html"} {
		r := get(p)
		if r.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" || r.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s pode ser embutido: csp=%q xfo=%q", p, r.Header().Get("Content-Security-Policy"), r.Header().Get("X-Frame-Options"))
		}
	}
	t.Setenv("WACALLS_EMBED_ORIGINS", "")
	if csp := get("/embed/chatwoot").Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'none'" {
		t.Fatalf("sem origens configuradas o embed deveria ficar fechado: %q", csp)
	}
}

// Ambiente dá o padrão; o que o admin salva vence; entradas inválidas caem.
func TestEmbedSettingsMerge(t *testing.T) {
	t.Setenv("WACALLS_EMBED_INBOXES", "12=sessA, 13=sessB, lixo")
	t.Setenv("WACALLS_EMBED_PHONE_ATTRS", "")
	e, _ := newEmbedSettings(context.Background(), nil)
	c := e.get()
	if c.Inboxes["12"] != "sessA" || c.Inboxes["13"] != "sessB" || len(c.Inboxes) != 2 {
		t.Fatalf("inboxes do ambiente = %v", c.Inboxes)
	}
	if strings.Join(c.PhoneAttrs, ",") != "waha_whatsapp_jid,waha_whatsapp_chat_id,waha_whatsapp_lid" {
		t.Fatalf("atributos padrão = %v", c.PhoneAttrs)
	}
	_ = e.set(context.Background(), embedConfig{Inboxes: map[string]string{"13": "sessC", "abc": "x", "14": ""}, PhoneAttrs: []string{" meu_attr "}})
	c = e.get()
	if c.Inboxes["13"] != "sessC" || c.Inboxes["12"] != "sessA" || len(c.Inboxes) != 2 || strings.Join(c.PhoneAttrs, ",") != "meu_attr" {
		t.Fatalf("config salva = %+v", c)
	}
}

// O atendente lê a config do embed e resolve LID nas sessões dele; não grava.
func TestEmbedAgentRoutes(t *testing.T) {
	a := &agent{Sessions: []string{"s1"}}
	ok := func(m, p string) bool { return agentAllowed(httptest.NewRequest(m, p, nil), a) }
	if !ok("GET", "/api/embed/config") || !ok("GET", "/api/sessions/s1/lid-phone") {
		t.Fatal("atendente deveria ler config e resolver LID")
	}
	if ok("PUT", "/api/embed/config") || ok("GET", "/api/sessions/s2/lid-phone") {
		t.Fatal("atendente não grava config nem usa sessão alheia")
	}
}
