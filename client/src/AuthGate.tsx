import { useEffect, useState } from "react";
import { App } from "./App";
import { EmbedChatwoot } from "./pages/EmbedChatwoot";
import { LoginScreen } from "./components/auth/LoginScreen";
import { basePath, checkAuth } from "@/lib/auth";

export const AuthGate = () => {
  const [state, setState] = useState<"checking" | "login" | "ok">("checking");

  useEffect(() => {
    checkAuth().then((ok) => setState(ok ? "ok" : "login"));
  }, []);

  if (state === "checking") return null;
  if (state === "login") return <LoginScreen onSuccess={() => setState("ok")} />;
  // aba Ligações dentro da conversa do Chatwoot (Dashboard App)
  if (location.pathname.slice(basePath().length).startsWith("/embed/chatwoot")) return <EmbedChatwoot />;
  return <App />;
};
