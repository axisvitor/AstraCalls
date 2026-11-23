import { create } from "zustand";

export type AgentInfo = { id: string; username: string; name: string; sessions: string[] };

export type AuthInfo = {
  /** admin = chave-mestra; agent = atendente (usuário/senha). */
  role: "admin" | "agent";
  /** Servidor em modo só ligações (WACALLS_CALLS_ONLY). */
  callsOnly: boolean;
  agent?: AgentInfo;
};

export const useAuthInfo = create<AuthInfo>(() => ({ role: "admin", callsOnly: false }));

export const setAuthInfo = (info: AuthInfo): void => useAuthInfo.setState(info, true);

export const useIsAgent = (): boolean => useAuthInfo((s) => s.role === "agent");

/** Painel enxuto (só o que serve para ligar): atendente, ou instância em modo só ligações. */
export const useLean = (): boolean => useAuthInfo((s) => s.role === "agent" || s.callsOnly);
