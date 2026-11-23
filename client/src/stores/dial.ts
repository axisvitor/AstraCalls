import { create } from "zustand";
import { setActiveSession, useSessions } from "@/stores/sessions";

/** Número a preencher no discador (vindo da aba Ligações do Chatwoot). */
export const useDialPrefill = create<{ phone: string | null }>(() => ({ phone: null }));

/**
 * Lê "#dial=<número>&sid=<sessão>" (aberto pela aba do Chatwoot), seleciona a
 * sessão, preenche o discador e limpa o hash. Usa hash, não query: abrir de novo
 * a mesma aba só troca o hash e NÃO recarrega a página (não derruba chamada).
 */
export const consumeDialHash = (): void => {
  const h = new URLSearchParams(location.hash.replace(/^#/, ""));
  const phone = (h.get("dial") ?? "").replace(/\D/g, "");
  if (!phone) return;
  const sid = h.get("sid") ?? "";
  const apply = () => {
    if (sid && useSessions.getState().sessions.some((s) => s.id === sid)) setActiveSession(sid);
  };
  apply();
  // a lista de sessões pode ainda não ter chegado
  const off = useSessions.subscribe(apply);
  setTimeout(off, 5000);
  useDialPrefill.setState({ phone });
  history.replaceState(null, "", location.pathname + location.search);
  window.focus();
};
