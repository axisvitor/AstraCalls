import { apiDelete, apiGet, apiPost, apiPut } from "@/lib/api";
import type { AgentInfo } from "@/stores/auth";

export type AgentInput = { username?: string; name?: string; password?: string; sessions?: string[] };

export const listAgents = () => apiGet<{ agents: AgentInfo[] }>("/api/agents").then((r) => r.agents ?? []);
export const createAgent = (a: AgentInput) => apiPost<{ agent: AgentInfo }>("/api/agents", a);
export const updateAgent = (id: string, a: AgentInput) => apiPut<{ agent: AgentInfo }>(`/api/agents/${id}`, a);
export const deleteAgent = (id: string) => apiDelete(`/api/agents/${id}`);
