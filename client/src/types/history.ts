export type HistoryRow = {
  callId: string;
  peer: string;
  direction: string;
  startedAt: number;
  endedAt: number | null;
  endReason: string | null;
  /** Recebida e não atendida (perdida, recusada ou expirada). */
  missed?: boolean;
  /** Atendente que atendeu/ligou. */
  agent?: string;
};
