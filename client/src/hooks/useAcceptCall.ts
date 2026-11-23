import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { openAdaptiveCall as openCall } from "@/lib/webrtc";
import { acceptCall, endCall } from "@/services/calls";
import { registerOwnConnection, clearIncoming } from "@/stores/calls";
import { MIC_DENIED_MSG, micDenied } from "@/lib/mic";

export const useAcceptCall = (micId: string | null) =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string; video: boolean }) => {
      const res = await acceptCall(vars.sid, vars.callId);
      try {
        const conn = await openCall(vars.sid, res.call.callId, micId, { video: vars.video });
        registerOwnConnection(res.call.callId, conn, vars.video);
      } catch (wrtcErr) {
        try {
          await endCall(vars.sid, res.call.callId);
        } catch {}
        throw wrtcErr;
      }
      clearIncoming();
      return res.call.callId;
    },
    onError: (e: Error) => {
      if (e.message.includes("409")) {
        clearIncoming();
        return;
      }
      toast.error(micDenied(e) ? MIC_DENIED_MSG : e.message);
    },
  });
