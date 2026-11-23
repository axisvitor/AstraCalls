import { useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { agentLogin, checkAuth, defaultBaseUrl, setAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";

type Mode = "agent" | "admin";

export const LoginScreen = ({ onSuccess }: { onSuccess: () => void }) => {
  const [mode, setMode] = useState<Mode>("agent");
  const [url, setUrl] = useState(defaultBaseUrl);
  const [user, setUser] = useState("");
  const [secret, setSecret] = useState(""); // senha (atendente) ou API key (admin)
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const submit = async () => {
    setBusy(true);
    setErr("");
    const base = url.replace(/\/+$/, "");
    try {
      if (mode === "agent") {
        await agentLogin(base, user.trim(), secret);
      } else {
        const r = await fetch(`${base}/api/config`, { headers: { "X-API-Key": secret } });
        if (!r.ok) {
          const msg = ((await r.json().catch(() => ({}))) as { error?: string }).error;
          setErr(r.status === 401 ? "API key inválida" : msg || `Erro ${r.status}`);
          return;
        }
        setAuth(base, secret);
      }
      if (await checkAuth()) onSuccess();
      else setErr("Não foi possível entrar");
    } catch (e) {
      setErr(e instanceof TypeError ? "Não foi possível conectar à URL informada" : (e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = !!secret.trim() && (mode === "admin" || !!user.trim());

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm space-y-5 rounded-2xl border bg-card p-7 shadow-soft">
        <div className="flex flex-col items-center gap-2 text-center">
          <span className="inline-flex dark:rounded-xl dark:bg-white dark:px-3 dark:py-2">
            <img src="logoCalls.png" alt="AstraCalls" className="h-9 w-auto select-none" draggable={false} />
          </span>
        </div>
        <div className="grid grid-cols-2 gap-1 rounded-xl bg-muted p-1" role="tablist" aria-label="Tipo de acesso">
          {(["agent", "admin"] as const).map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={mode === m}
              onClick={() => {
                setMode(m);
                setErr("");
                setSecret("");
              }}
              className={cn(
                "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
                mode === m ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {m === "agent" ? "Atendente" : "Administrador"}
            </button>
          ))}
        </div>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit && !busy) void submit();
          }}
        >
          {mode === "admin" && (
            <div className="space-y-1">
              <Label htmlFor="login-url">URL</Label>
              <Input id="login-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://call.seudominio.com" />
            </div>
          )}
          {mode === "agent" && (
            <div className="space-y-1">
              <Label htmlFor="login-user">Usuário</Label>
              <Input
                id="login-user"
                value={user}
                onChange={(e) => setUser(e.target.value)}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>
          )}
          <div className="space-y-1">
            <Label htmlFor="login-secret">{mode === "agent" ? "Senha" : "API key"}</Label>
            <Input
              id="login-secret"
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              autoComplete={mode === "agent" ? "current-password" : "off"}
            />
          </div>
          {err && (
            <p className="text-sm text-destructive" role="alert">
              {err}
            </p>
          )}
          <Button type="submit" className="w-full" disabled={busy || !canSubmit}>
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            Entrar
          </Button>
        </form>
      </div>
    </div>
  );
};
