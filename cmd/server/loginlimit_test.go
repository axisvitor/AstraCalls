package main

import (
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 60 logins em paralelo no mesmo usuário: no máximo agentMaxFails senhas são
// conferidas (antes passavam dezenas), e a senha certa no meio não entra.
func TestAgentLoginParallelLimit(t *testing.T) {
	d, _ := newTestAgents(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked, okCount := 0, 0
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pw := "errada"
			if i == 30 {
				pw = "senha-forte-1"
			}
			_, _, _, err := d.login("maria", pw, "198.51.100.7")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				okCount++
			}
			if !errors.Is(err, errLoginLocked) {
				checked++
			}
		}(i)
	}
	wg.Wait()
	if checked > agentMaxFails {
		t.Fatalf("%d senhas conferidas em paralelo, limite %d", checked, agentMaxFails)
	}
	if _, _, _, err := d.login("maria", "senha-forte-1", "198.51.100.8"); !errors.Is(err, errLoginLocked) && okCount == 0 {
		t.Fatalf("depois da rajada o usuário deveria estar travado: %v", err)
	}
}

// Um IP que erra muito (em usuários quaisquer) fica travado, inclusive para um
// usuário que ele ainda não tentou; outro IP segue entrando.
func TestAgentLoginPerIP(t *testing.T) {
	d, _ := newTestAgents(t)
	for i := 0; i < agentMaxFailsIP; i++ {
		_, _, _, _ = d.login("inventado"+string(rune('a'+i)), "x", "203.0.113.9")
	}
	if _, _, _, err := d.login("maria", "senha-forte-1", "203.0.113.9"); !errors.Is(err, errLoginLocked) {
		t.Fatalf("IP abusivo deveria estar travado: %v", err)
	}
	if _, _, _, err := d.login("maria", "senha-forte-1", "203.0.113.10"); err != nil {
		t.Fatalf("outro IP deveria entrar: %v", err)
	}
}

// Os erros valem por uma janela: 19 erros do IP da loja (vendedores errando a
// senha ao longo do dia) não somam com o 20º meia hora depois.
func TestAgentLoginIPWindowDecays(t *testing.T) {
	d, _ := newTestAgents(t)
	now := time.Now()
	d.now = func() time.Time { return now }
	for i := 0; i < agentMaxFailsIP-1; i++ {
		_, _, _, _ = d.login("u"+string(rune('a'+i)), "x", "203.0.113.50")
	}
	now = now.Add(agentLockFor + time.Minute)
	_, _, _, _ = d.login("outro", "x", "203.0.113.50")
	if _, _, _, err := d.login("maria", "senha-forte-1", "203.0.113.50"); err != nil {
		t.Fatalf("erros antigos não deveriam travar o IP: %v", err)
	}
}

// X-Forwarded-For só vale vindo de proxy confiável (senão qualquer um troca de IP
// a cada pedido e foge do limite).
func TestClientIPTrustedProxy(t *testing.T) {
	req := func(remote, xff string) string {
		r := httptest.NewRequest("POST", "/api/agent/login", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return clientIP(r)
	}
	t.Setenv("WACALLS_TRUSTED_PROXIES", "")
	if got := req("203.0.113.5:1234", "1.2.3.4"); got != "203.0.113.5" {
		t.Fatalf("sem proxy confiável o XFF não vale: %s", got)
	}
	t.Setenv("WACALLS_TRUSTED_PROXIES", "172.16.0.0/12, 10.0.0.9")
	if got := req("172.18.0.3:5555", "6.6.6.6, 198.51.100.4"); got != "198.51.100.4" {
		t.Fatalf("via Caddy confiável = %s, quer o último IP antes do proxy", got)
	}
	if got := req("10.0.0.9:1", "198.51.100.4, 172.18.0.2"); got != "198.51.100.4" {
		t.Fatalf("pula proxies confiáveis da cadeia: %s", got)
	}
	if got := req("203.0.113.5:1", "198.51.100.4"); got != "203.0.113.5" {
		t.Fatalf("conexão direta forjando XFF = %s", got)
	}
}

// Com as vagas de bcrypt ocupadas, o login responde "ocupado" em vez de enfileirar
// sem fim, e isso não conta como senha errada.
func TestAgentLoginBcryptSlots(t *testing.T) {
	d, _ := newTestAgents(t)
	prev := bcryptWait
	bcryptWait = 50 * time.Millisecond
	t.Cleanup(func() { bcryptWait = prev })
	for i := 0; i < cap(bcryptSlots); i++ {
		bcryptSlots <- struct{}{}
	}
	_, _, _, err := d.login("maria", "senha-forte-1", "198.51.100.20")
	for i := 0; i < cap(bcryptSlots); i++ {
		<-bcryptSlots
	}
	if !errors.Is(err, errLoginBusy) {
		t.Fatalf("com as vagas cheias: %v", err)
	}
	if _, _, _, err := d.login("maria", "senha-forte-1", "198.51.100.20"); err != nil {
		t.Fatalf("depois de liberar: %v", err)
	}
}
