package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type server struct {
	broker    *Broker
	sessions  *SessionManager
	log       *slog.Logger
	staticDir string
	sipGW     *SIPGateway
	agents    *agentDirectory // atendentes (usuário/senha) — ver agents.go
	embed     *embedSettings  // aba Ligações no Chatwoot — ver embed.go
	apiMux    *http.ServeMux  // usado pelo MCP para despachar as ferramentas em processo
}

// newServer monta o provedor de banco (Postgres, 1 banco por sessão no estilo
// WAHA), abre o banco principal e inicializa o gerenciador de sessões.
func newServer(ctx context.Context, pgURL, pgNamespace, staticDir string, maxCalls int, log *slog.Logger) (*server, error) {
	waLogger := waLog.Noop
	if log.Enabled(ctx, slog.LevelDebug) {
		waLogger = waLog.Stdout("WA", "DEBUG", true)
	}

	provider, err := newDBProvider(ctx, pgURL, pgNamespace, waLogger, log)
	if err != nil {
		return nil, err
	}

	mainDB, err := provider.openMainDB(ctx)
	if err != nil {
		return nil, err
	}
	store, err := newSessionStore(ctx, mainDB)
	if err != nil {
		return nil, err
	}

	agents, err := newAgentDirectory(ctx, mainDB)
	if err != nil {
		return nil, err
	}

	embed, err := newEmbedSettings(ctx, mainDB)
	if err != nil {
		return nil, err
	}

	broker := NewBroker()
	if rows, err := store.recentCalls(ctx, callHistoryLoad); err != nil {
		log.Warn("histórico de chamadas: não carregou do banco", "err", err)
	} else {
		broker.loadHistory(rows)
	}
	broker.OnEnded = func(rec CallRecord) {
		go func() {
			if err := store.saveCall(context.Background(), rec); err != nil {
				log.Warn("histórico de chamadas: não gravou", "call_id", rec.CallID, "err", err)
			}
		}()
	}
	mgr := newSessionManager(ctx, provider, broker, store, waLogger, log, maxCalls)
	broker.SessionsFn = mgr.infos
	broker.AccountForSession = mgr.accountIDForSession
	broker.HostForSession = mgr.hostForSession
	broker.InboxForSession = mgr.inboxIDForSession

	// Gateway SIP: o PBX/softphone do cliente se registra aqui (modelo Wavoip).
	srv := &server{broker: broker, sessions: mgr, log: log, staticDir: staticDir, agents: agents, embed: embed}
	sipAddr, sipOn := sipListenAddr()
	if !sipOn {
		log.Info("SIP desativado (WACALLS_SIP_ADDR=off ou modo só ligações): porta SIP não aberta")
		return srv, nil
	}
	sipGateway, err := NewSIPGateway(mgr, log)
	if err != nil {
		log.Error("SIP gateway init failed", "err", err)
	} else {
		mgr.sipInbound = sipGateway.handleInboundCall
		mgr.sipAgentCount = sipGateway.agentCount
		mgr.sipExtApply = sipGateway.applyExtRegistration
		mgr.sipExtRemove = sipGateway.removeExtRegistration
		_ = sipGateway.Start(ctx, sipAddr)
		srv.sipGW = sipGateway
	}
	return srv, nil
}

// sipListenAddr devolve onde o gateway SIP escuta e se ele deve subir. Endereço por
// WACALLS_SIP_ADDR (padrão 0.0.0.0:5060); "off"/"0"/"false" desliga. No modo só
// ligações o padrão é desligado: só sobe se WACALLS_SIP_ADDR for setado.
func sipListenAddr() (string, bool) {
	addr := strings.TrimSpace(os.Getenv("WACALLS_SIP_ADDR"))
	switch strings.ToLower(addr) {
	case "off", "0", "false", "no", "disabled":
		return "", false
	case "":
		if callsOnly {
			return "", false
		}
		return "0.0.0.0:5060", true
	}
	return addr, true
}
