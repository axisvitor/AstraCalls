import { setAuthInfo, type AuthInfo } from "@/stores/auth";

const URL_KEY = "wacalls.apiUrl";
const KEY_KEY = "wacalls.apiKey";

/** Prefixo de caminho do painel atrás do proxy (ex.: "/astracalls"); injetado pelo servidor. */
export const basePath = (): string => (window as unknown as { __WACALLS_BASE?: string }).__WACALLS_BASE ?? "";

/** URL do painel/API: a salva no login, ou esta origem com o prefixo. */
export const defaultBaseUrl = (): string => window.location.origin + basePath();

export const getApiBase = (): string => (localStorage.getItem(URL_KEY) || defaultBaseUrl()).replace(/\/+$/, "");
export const getApiKey = (): string => localStorage.getItem(KEY_KEY) || "";
export const isAuthed = (): boolean => !!getApiKey();

export const setAuth = (url: string, key: string): void => {
  localStorage.setItem(URL_KEY, url.replace(/\/+$/, ""));
  localStorage.setItem(KEY_KEY, key);
};

export const clearAuth = (): void => {
  localStorage.removeItem(KEY_KEY);
};

export const apiUrl = (path: string): string => getApiBase() + path;

// checkAuth verifica se o backend aceita (servidor aberto -> 200 mesmo sem key) e
// guarda o papel (admin/atendente) e o modo só ligações vindos de /api/config.
export const checkAuth = async (): Promise<boolean> => {
  try {
    const r = await fetch(apiUrl("/api/config"), { headers: { "X-API-Key": getApiKey() } });
    if (!r.ok) return false;
    const cfg = (await r.json()) as { role?: string; callsOnly?: boolean; agent?: AuthInfo["agent"] };
    setAuthInfo({ role: cfg.role === "agent" ? "agent" : "admin", callsOnly: !!cfg.callsOnly, agent: cfg.agent });
    return true;
  } catch {
    return false;
  }
};

/** Renova o token do atendente de hora em hora enquanto a aba estiver aberta. */
export const startAgentTokenRefresh = (): void => {
  setInterval(() => {
    fetch(apiUrl("/api/agent/refresh"), { method: "POST", headers: { "X-API-Key": getApiKey() } })
      .then((r) => (r.ok ? (r.json() as Promise<{ token?: string }>) : null))
      .then((b) => {
        if (b?.token) setAuth(getApiBase(), b.token);
      })
      .catch(() => {});
  }, 60 * 60 * 1000);
};

/** Login de atendente: troca usuário/senha por um token (usado como API key). */
export const agentLogin = async (base: string, username: string, password: string): Promise<void> => {
  const r = await fetch(`${base}/api/agent/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  const body = (await r.json().catch(() => ({}))) as { token?: string; error?: string };
  if (!r.ok || !body.token) {
    throw new Error(r.status === 401 ? "Usuário ou senha inválidos" : body.error || `Erro ${r.status}`);
  }
  setAuth(base, body.token);
};
