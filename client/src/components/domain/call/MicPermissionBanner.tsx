import { useEffect, useState } from "react";
import { MicOff } from "lucide-react";
import { Button } from "@/components/ui/button";

type MicState = "granted" | "denied" | "prompt" | "unknown";

/**
 * Avisa quando o microfone não está liberado (sem ele não dá para falar) e
 * oferece pedir a permissão antes da primeira chamada. Navegadores que não
 * expõem a permissão "microphone" (ex.: Firefox) ficam em "unknown" e o aviso
 * só aparece se o pedido falhar.
 */
export const MicPermissionBanner = () => {
  const [state, setState] = useState<MicState>("unknown");

  useEffect(() => {
    let status: PermissionStatus | null = null;
    const update = () => status && setState(status.state as MicState);
    navigator.permissions
      ?.query({ name: "microphone" as PermissionName })
      .then((s) => {
        status = s;
        update();
        s.addEventListener("change", update);
      })
      .catch(() => {});
    return () => status?.removeEventListener("change", update);
  }, []);

  const ask = async () => {
    try {
      const s = await navigator.mediaDevices.getUserMedia({ audio: true });
      s.getTracks().forEach((t) => t.stop());
      setState("granted");
    } catch {
      setState("denied");
    }
  };

  if (state === "granted" || state === "unknown") return null;
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm"
    >
      <MicOff className="h-5 w-5 shrink-0 text-destructive" />
      <p className="min-w-0 flex-1">
        {state === "denied" ? (
          <>
            <strong>Microfone bloqueado.</strong> Sem ele o cliente não ouve você. Clique no cadeado ao lado do endereço
            do site, permita o microfone e recarregue a página.
          </>
        ) : (
          <>
            <strong>Microfone ainda não liberado.</strong> Libere agora para não perder tempo quando o telefone tocar.
          </>
        )}
      </p>
      {state === "prompt" && (
        <Button size="sm" onClick={ask}>
          Permitir microfone
        </Button>
      )}
    </div>
  );
};
