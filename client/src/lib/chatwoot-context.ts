/**
 * Contrato público do Dashboard App do Chatwoot (aba dentro da conversa). O Chatwoot
 * manda ao iframe, por postMessage, uma STRING JSON:
 *   {"event":"appContext","data":{"conversation":{...},"contact":{...},"currentAgent":{...}}}
 * e reenvia quando o app manda a string FETCH_INFO para window.parent.
 * Sem imports de "@/": este arquivo roda também no teste do Node.
 */

export const FETCH_INFO = "chatwoot-dashboard-app:fetch-info";

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj | null => (v && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : null);
const str = (v: unknown): string => (typeof v === "string" ? v : "");

export type EmbedContact = {
  name: string;
  /** Só dígitos (com DDI), pronto para ligar. Vazio se não deu para descobrir. */
  phone: string;
  /** JID @lid do contato, quando só ele existe (o servidor tenta achar o número). */
  lid: string;
  inboxId: number | null;
  conversationId: number | null;
};

const normOrigin = (o: string) => o.trim().replace(/\/+$/, "").toLowerCase();

/**
 * Valida uma mensagem recebida e devolve o `data` do appContext, ou null. Ignora:
 * origem fora da lista, remetente que não é a janela esperada (o Chatwoot pai),
 * dado que não é string JSON e evento diferente de appContext. Nunca lê
 * credenciais daqui.
 */
export const parseAppContextMessage = (
  ev: { origin: string; data: unknown; source?: unknown },
  allowedOrigins: string[],
  expectedSource?: unknown,
): Obj | null => {
  if (!allowedOrigins.map(normOrigin).includes(normOrigin(ev.origin || ""))) return null;
  if (expectedSource !== undefined && ev.source !== expectedSource) return null;
  if (typeof ev.data !== "string" || ev.data.length > 1_000_000) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(ev.data);
  } catch {
    return null;
  }
  const msg = obj(parsed);
  if (!msg || msg.event !== "appContext") return null;
  return obj(msg.data);
};

const digits = (s: string) => s.replace(/\D/g, "");

/** Lê um JID/telefone: "5594...@c.us", "...@s.whatsapp.net", "55..:12@s.whatsapp.net", "+55 94 ...", "123@lid". */
const readNumber = (raw: string): { phone: string; lid: string } => {
  const v = raw.trim();
  if (!v) return { phone: "", lid: "" };
  const at = v.indexOf("@");
  if (at < 0) {
    const d = digits(v);
    return { phone: d.length >= 8 ? d : "", lid: "" };
  }
  const user = v.slice(0, at).split(":")[0];
  const server = v.slice(at + 1).toLowerCase();
  if (server === "lid") return { phone: "", lid: digits(user) ? `${digits(user)}@lid` : "" };
  if (server === "c.us" || server === "s.whatsapp.net") {
    const d = digits(user);
    return { phone: d.length >= 8 ? d : "", lid: "" };
  }
  return { phone: "", lid: "" }; // grupo, canal etc.: não é número de pessoa
};

const num = (v: unknown): number | null => (typeof v === "number" && Number.isFinite(v) ? v : null);

/**
 * Extrai do appContext o contato e o número: phone_number e, na falta, os
 * atributos personalizados em `attrs` (nessa ordem). Campos ausentes viram vazio.
 */
export const contactFromContext = (data: Obj | null, attrs: string[]): EmbedContact => {
  const conversation = obj(data?.conversation);
  const contact = obj(data?.contact) ?? obj(obj(conversation?.meta)?.sender);
  const out: EmbedContact = {
    name: str(contact?.name),
    phone: "",
    lid: "",
    inboxId: num(conversation?.inbox_id),
    conversationId: num(conversation?.id),
  };
  const fromPhone = readNumber(str(contact?.phone_number));
  out.phone = fromPhone.phone;
  const custom = obj(contact?.custom_attributes);
  for (const a of attrs) {
    if (out.phone) break;
    const r = readNumber(str(custom?.[a]));
    out.phone = r.phone;
    if (!out.lid) out.lid = r.lid;
  }
  return out;
};

/** Mesmo telefone, aceitando o 9º dígito brasileiro presente ou não. */
export const samePhone = (a: string, b: string): boolean => {
  const x = digits(a);
  const y = digits(b);
  if (!x || !y) return false;
  if (x === y) return true;
  const br = (d: string) => (d.startsWith("55") && d.length === 13 && d[4] === "9" ? d.slice(0, 4) + d.slice(5) : d);
  return br(x) === br(y);
};
