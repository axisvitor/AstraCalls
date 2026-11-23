import { useEffect, useMemo, useState } from "react";
import { ExternalLink, Phone, PhoneIncoming, PhoneMissed, PhoneOutgoing, TriangleAlert } from "lucide-react";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CallCard } from "@/components/domain/call/CallCard";
import { MicPermissionBanner } from "@/components/domain/call/MicPermissionBanner";
import { apiGet } from "@/lib/api";
import { defaultBaseUrl } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { FETCH_INFO, contactFromContext, parseAppContextMessage, samePhone, type EmbedContact } from "@/lib/chatwoot-context";
import { useHistory } from "@/hooks/useHistory";
import { useStartCall } from "@/hooks/useStartCall";
import { ensureCallsWired, isMine, useCalls } from "@/stores/calls";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import { useDevices } from "@/stores/devices";
import { phoneFromJid } from "@/utils/format";

type EmbedConfig = { inboxes: Record<string, string>; phoneAttrs: string[] };

const inIframe = window.self !== window.top;

// O Chatwoot é o pai e tem a mesma origem? (painel servido sob um caminho do
// domínio do Chatwoot). Em origem diferente, ler window.parent.location lança.
const sameOriginParent = (): boolean => {
  try {
    return window.parent.location.origin === window.location.origin;
  } catch {
    return false;
  }
};

// Dá para usar o microfone AQUI? O iframe do Chatwoot não tem `allow`, então só em
// mesma origem (ou fora de iframe). Detectado em tempo de execução, nunca assumido.
const micUsableHere = (): boolean => {
  if (inIframe && !sameOriginParent()) return false;
  const policy = (document as unknown as { permissionsPolicy?: { allowsFeature: (f: string) => boolean } })
    .permissionsPolicy ??
    (document as unknown as { featurePolicy?: { allowsFeature: (f: string) => boolean } }).featurePolicy;
  return policy ? policy.allowsFeature("microphone") : true;
};

const allowedOrigins = (): string[] =>
  (window as unknown as { __WACALLS_EMBED_ORIGINS?: string[] }).__WACALLS_EMBED_ORIGINS ?? [];

const PANEL_WINDOW = "astracalls-painel";

/** Abre (ou foca) a aba do painel completo com sessão e número preenchidos. */
const openPanel = (sid: string, phone: string) => {
  const url = `${defaultBaseUrl()}/#dial=${encodeURIComponent(phone)}&sid=${encodeURIComponent(sid)}`;
  const w = window.open(url, PANEL_WINDOW);
  w?.focus();
  return !!w;
};

export const EmbedChatwoot = () => {
  const [cfg, setCfg] = useState<EmbedConfig | null>(null);
  const [ctxData, setCtxData] = useState<Record<string, unknown> | null>(null);
  const [waited, setWaited] = useState(false);
  const [manual, setManual] = useState("");
  const [lidPhone, setLidPhone] = useState("");
  const [pickedSid, setPickedSid] = useState("");
  const [blocked, setBlocked] = useState(false);
  const sessions = useSessions((s) => s.sessions);
  const calls = useCalls((s) => s.calls);
  const micId = useDevices((s) => s.micId);
  const inline = useMemo(micUsableHere, []);

  useEffect(() => {
    ensureSessionsWired();
    ensureCallsWired();
    apiGet<EmbedConfig>("/api/embed/config")
      .then(setCfg)
      .catch(() => setCfg({ inboxes: {}, phoneAttrs: [] }));
    const onMsg = (ev: MessageEvent) => {
      const data = parseAppContextMessage(ev, allowedOrigins(), window.parent);
      if (data) setCtxData(data);
    };
    window.addEventListener("message", onMsg);
    // pede o contexto de novo (o 1º envio pode ter chegado antes do login)
    if (inIframe) window.parent.postMessage(FETCH_INFO, "*");
    const t = setTimeout(() => setWaited(true), 2500);
    return () => {
      window.removeEventListener("message", onMsg);
      clearTimeout(t);
    };
  }, []);

  const contact: EmbedContact | null = useMemo(
    () => (ctxData && cfg ? contactFromContext(ctxData, cfg.phoneAttrs) : null),
    [ctxData, cfg],
  );

  // sessão: pela caixa de entrada da conversa; senão a única liberada; senão escolha.
  const mappedSid = contact?.inboxId != null ? cfg?.inboxes[String(contact.inboxId)] : undefined;
  const sid =
    pickedSid ||
    (mappedSid && sessions.some((s) => s.id === mappedSid) ? mappedSid : "") ||
    (sessions.length === 1 ? sessions[0].id : "");
  const mappedButNoAccess = !!mappedSid && sessions.length > 0 && !sessions.some((s) => s.id === mappedSid);

  // só @lid: pergunta ao servidor se ele conhece o número desse LID nesta sessão
  useEffect(() => {
    setLidPhone("");
    if (!contact?.lid || contact.phone || !sid) return;
    apiGet<{ phone: string }>(`/api/sessions/${sid}/lid-phone?lid=${encodeURIComponent(contact.lid)}`)
      .then((r) => setLidPhone(r.phone))
      .catch(() => {});
  }, [contact?.lid, contact?.phone, sid]);

  const phone = (manual || contact?.phone || lidPhone).replace(/\D/g, "");
  const startCall = useStartCall(sid, micId);
  const { data: rows = [] } = useHistory(sid || null, !!sid);
  const history = phone ? rows.filter((r) => samePhone(r.peer.split("@")[0], phone)) : [];
  const myCalls = calls.filter((c) => c.sessionId === sid && c.status !== "ended" && isMine(c));

  const call = () => {
    if (!sid || !phone) return;
    if (inline) startCall.mutate({ phone, record: false, video: false });
    else setBlocked(!openPanel(sid, phone));
  };

  const noContext = !ctxData && waited;
  const noNumber = !!ctxData && !contact?.phone && !lidPhone && !manual;

  return (
    <TooltipProvider delayDuration={200}>
      <div className="min-h-screen space-y-4 bg-background p-4 text-sm">
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Cliente</p>
          <p className="truncate text-lg font-semibold">{contact?.name || "—"}</p>
          <p className="text-muted-foreground">{phone ? phoneFromJid(phone) : "sem número"}</p>
        </div>

        {(noContext || noNumber) && (
          <div role="status" className="flex gap-2 rounded-xl border border-amber-500/40 bg-amber-500/10 p-3">
            <TriangleAlert className="h-5 w-5 shrink-0 text-amber-600" />
            <p>
              {noContext
                ? "Não recebemos os dados da conversa do Chatwoot. Digite o número abaixo para ligar."
                : contact?.lid
                  ? "Este contato só tem o identificador interno do WhatsApp (@lid) e o número dele ainda não é conhecido. Digite o número abaixo para ligar."
                  : "Não encontramos o número de WhatsApp deste contato. Digite o número abaixo para ligar."}
            </p>
          </div>
        )}

        {(noContext || noNumber || manual) && (
          <div className="space-y-1">
            <Label htmlFor="embed-phone">Número (com DDD)</Label>
            <Input
              id="embed-phone"
              inputMode="tel"
              placeholder="+55 11 99999 9999"
              value={manual}
              onChange={(e) => setManual(e.target.value)}
            />
          </div>
        )}

        {sessions.length !== 1 && !mappedSid && (
          <div className="space-y-1">
            <Label htmlFor="embed-sid">Ligar pela linha</Label>
            <select
              id="embed-sid"
              className="h-9 w-full rounded-full border border-input bg-card px-3"
              value={sid}
              onChange={(e) => setPickedSid(e.target.value)}
            >
              <option value="">Escolha…</option>
              {sessions.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} {phoneFromJid(s.jid || s.lastJid)}
                </option>
              ))}
            </select>
          </div>
        )}
        {mappedButNoAccess && (
          <p className="text-destructive">A linha desta caixa de entrada não está liberada para o seu usuário.</p>
        )}
        {sessions.length === 0 && <p className="text-destructive">Nenhuma linha liberada para o seu usuário.</p>}

        {inline && <MicPermissionBanner />}
        <Button className="w-full" disabled={!sid || !phone || startCall.isPending} onClick={call}>
          {inline ? <Phone className="h-4 w-4" /> : <ExternalLink className="h-4 w-4" />}
          Ligar para este cliente
        </Button>
        {!inline && (
          <p className="text-xs text-muted-foreground">
            A ligação acontece na aba do painel do AstraCalls (o navegador não libera o microfone dentro do Chatwoot).
            Chamadas recebidas também tocam lá: deixe aquela aba aberta.
          </p>
        )}
        {blocked && (
          <p className="text-destructive">
            O navegador bloqueou a abertura da aba. Permita pop-ups para o Chatwoot ou abra{" "}
            <a className="underline" href={defaultBaseUrl() + "/"} target={PANEL_WINDOW} rel="noopener">
              o painel
            </a>
            .
          </p>
        )}

        {inline && myCalls.map((c) => <CallCard key={c.callId} call={c} />)}

        <div className="space-y-2">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">Ligações com este número</p>
          {history.length === 0 ? (
            <p className="text-muted-foreground">Nenhuma ligação ainda.</p>
          ) : (
            <ul className="space-y-1.5">
              {history.slice(0, 20).map((r) => {
                const Icon = r.missed ? PhoneMissed : r.direction === "inbound" ? PhoneIncoming : PhoneOutgoing;
                return (
                  <li
                    key={r.callId}
                    className={cn("flex items-center gap-2 rounded-lg border p-2", r.missed && "border-destructive/40 bg-destructive/10")}
                  >
                    <Icon className={cn("h-4 w-4 shrink-0", r.missed ? "text-destructive" : "text-muted-foreground")} />
                    <span className={cn(r.missed && "font-semibold text-destructive")}>
                      {r.missed ? (r.endReason === "declined" ? "Recusada" : "Perdida") : r.direction === "inbound" ? "Recebida" : "Feita"}
                    </span>
                    <span className="text-muted-foreground">
                      {new Date(r.startedAt).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}
                      {r.agent ? ` · ${r.agent}` : ""}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
      <Toaster position="top-center" richColors />
    </TooltipProvider>
  );
};
