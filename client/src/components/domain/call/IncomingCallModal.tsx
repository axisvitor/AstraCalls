import { useEffect } from "react";
import { Phone, PhoneIncoming, PhoneOff } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useCalls } from "@/stores/calls";
import { useDevices } from "@/stores/devices";
import { useAcceptCall } from "@/hooks/useAcceptCall";
import { useRejectCall } from "@/hooks/useRejectCall";

type RingHandle = { stop: () => void };

const startRingLoop = (): RingHandle | null => {
  const AC = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AC) return null;
  let ctx: AudioContext;
  try {
    ctx = new AC();
  } catch {
    return null;
  }
  let cancelled = false;
  const playToneAt = (when: number, durationSec: number, freq: number, gainVal = 0.18) => {
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    const t = ctx.currentTime + when;
    gain.gain.setValueAtTime(0, t);
    gain.gain.linearRampToValueAtTime(gainVal, t + 0.02);
    gain.gain.linearRampToValueAtTime(gainVal, t + durationSec - 0.02);
    gain.gain.linearRampToValueAtTime(0, t + durationSec);
    osc.connect(gain).connect(ctx.destination);
    osc.start(t);
    osc.stop(t + durationSec + 0.05);
  };
  const scheduleCycle = () => {
    if (cancelled) return;
    playToneAt(0, 1.0, 440);
    playToneAt(0, 1.0, 480);
    setTimeout(scheduleCycle, 3000);
  };
  // sem um clique na página o navegador pode criar o contexto suspenso.
  void ctx.resume().catch(() => {});
  scheduleCycle();
  return {
    stop: () => {
      cancelled = true;
      void ctx.close().catch(() => {});
    },
  };
};

// Aviso do sistema quando a aba está em segundo plano. A permissão é pedida no
// primeiro clique na página (alguns navegadores só aceitam pedir num gesto).
const askNotifyOnFirstClick = (): void => {
  if (typeof Notification === "undefined" || Notification.permission !== "default") return;
  const ask = () => {
    document.removeEventListener("click", ask);
    void Notification.requestPermission().catch(() => {});
  };
  document.addEventListener("click", ask);
};

const notifyBackground = (peer: string, tag: string): (() => void) => {
  const prevTitle = document.title;
  document.title = "📞 Chamada recebida";
  let n: Notification | null = null;
  if (document.hidden && typeof Notification !== "undefined" && Notification.permission === "granted") {
    try {
      n = new Notification("Chamada recebida", { body: peer || "Número desconhecido", tag, requireInteraction: true });
      n.onclick = () => {
        window.focus();
        n?.close();
      };
    } catch {
      n = null;
    }
  }
  return () => {
    document.title = prevTitle;
    n?.close();
  };
};

export const IncomingCallModal = () => {
  const incoming = useCalls((s) => s.incoming);
  const micId = useDevices((s) => s.micId);
  const accept = useAcceptCall(micId);
  const reject = useRejectCall();
  const busy = accept.isPending || reject.isPending;

  useEffect(askNotifyOnFirstClick, []);

  useEffect(() => {
    if (!incoming) return;
    const ring = startRingLoop();
    const clearNotice = notifyBackground(incoming.peer, incoming.callId);
    return () => {
      ring?.stop();
      clearNotice();
    };
  }, [incoming]);

  return (
    <Dialog open={!!incoming}>
      <DialogContent
        showCloseButton={false}
        onEscapeKeyDown={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
        className="sm:max-w-sm"
      >
        <DialogHeader className="items-center text-center">
          <div className="mb-2 flex h-14 w-14 items-center justify-center rounded-full bg-primary/10 text-primary">
            <PhoneIncoming className="h-7 w-7" />
          </div>
          <DialogTitle>{incoming?.video ? "Chamada de vídeo recebida" : "Chamada recebida"}</DialogTitle>
          <DialogDescription className="truncate">{incoming?.peer || "Número desconhecido"}</DialogDescription>
        </DialogHeader>
        <div className="mt-2 flex items-center justify-center gap-6">
          <Button
            variant="destructive"
            size="icon"
            className="h-14 w-14 rounded-full"
            disabled={busy}
            onClick={() => incoming && reject.mutate({ sid: incoming.sessionId, callId: incoming.callId })}
            aria-label="Recusar"
          >
            <PhoneOff className="h-6 w-6" />
          </Button>
          <Button
            size="icon"
            className="h-14 w-14 rounded-full"
            disabled={busy}
            onClick={() => incoming && accept.mutate({ sid: incoming.sessionId, callId: incoming.callId, video: !!incoming.video })}
            aria-label="Atender"
          >
            <Phone className="h-6 w-6" />
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};
