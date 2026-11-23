import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openAdaptiveCall as openCall } from "@/lib/webrtc";
import { startCall } from "@/services/calls";
import { registerOwnConnection } from "@/stores/calls";
import { MIC_DENIED_MSG, micDenied } from "@/lib/mic";

export const useStartCall = (sid: string, micId: string | null) =>
  useMutation({
    mutationFn: async (vars: { phone: string; record: boolean; video: boolean }) => {
      const { call } = await startCall(sid, vars.phone, vars.record, vars.video);
      const conn = await openCall(sid, call.callId, micId, { video: vars.video });
      registerOwnConnection(call.callId, conn, vars.video);
      return call.callId;
    },
    onError: (e: Error) => {
      const m = e.message;
      if (micDenied(e)) toast.error(MIC_DENIED_MSG);
      else if (m.includes("429")) toast.error("Limite de chamadas simultâneas atingido.");
      else if (m.includes("503")) toast.error("A linha de WhatsApp não está conectada.");
      else toast.error(m);
    },
  });
