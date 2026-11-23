import { useState } from "react";
import { History, Phone, PhoneIncoming, PhoneMissed, PhoneOutgoing } from "lucide-react";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { EmptyState } from "@/components/shared/EmptyState";
import { useHistory } from "@/hooks/useHistory";
import { useStartCall } from "@/hooks/useStartCall";
import { useDevices } from "@/stores/devices";
import { cn } from "@/lib/utils";
import { phoneFromJid } from "@/utils/format";
import type { HistoryRow } from "@/types/history";

const seenKey = (sid: string) => `wacalls.missedSeen.${sid}`;
const readSeen = (sid: string): number => {
  try {
    return Number(localStorage.getItem(seenKey(sid))) || 0;
  } catch {
    return 0;
  }
};

const digits = (peer: string) => peer.split("@")[0].split(":")[0].replace(/\D/g, "");

const label = (r: HistoryRow): string => {
  if (r.missed) return r.endReason === "declined" ? "Recusada" : "Perdida";
  if (r.direction === "inbound") return r.endReason === "accepted_elsewhere" ? "Atendida em outro aparelho" : "Recebida";
  return "Feita";
};

const when = (ms: number) =>
  new Date(ms).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });

export const HistoryDrawer = ({ sid }: { sid: string }) => {
  const [open, setOpen] = useState(false);
  const [seen, setSeen] = useState(() => readSeen(sid));
  // sempre carregado: o contador de perdidas aparece no botão mesmo fechado.
  const { data: rows = [] } = useHistory(sid, true);
  const micId = useDevices((s) => s.micId);
  const startCall = useStartCall(sid, micId);
  const unseenMissed = rows.filter((r) => r.missed && r.startedAt > seen).length;

  const onOpenChange = (o: boolean) => {
    setOpen(o);
    if (o) {
      const now = Date.now();
      setSeen(now);
      try {
        localStorage.setItem(seenKey(sid), String(now));
      } catch {
        /* sem storage: o contador só não persiste */
      }
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm">
          <History className="h-4 w-4" />
          Histórico
          {unseenMissed > 0 && (
            <span
              className="ml-1 rounded-full bg-destructive px-1.5 text-xs font-semibold text-white"
              aria-label={`${unseenMissed} chamadas perdidas novas`}
            >
              {unseenMissed}
            </span>
          )}
        </Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <SheetHeader className="p-6 pb-4">
          <SheetTitle>Histórico de chamadas</SheetTitle>
        </SheetHeader>
        <Separator />
        <ScrollArea className="h-[calc(100vh-5.5rem)] px-6 py-4">
          {rows.length === 0 ? (
            <EmptyState title="Nenhuma chamada anterior" description="As chamadas feitas ou recebidas aparecem aqui." />
          ) : (
            <ul className="space-y-2">
              {rows.map((r) => {
                const num = digits(r.peer);
                const Icon = r.missed ? PhoneMissed : r.direction === "inbound" ? PhoneIncoming : PhoneOutgoing;
                return (
                  <li
                    key={r.callId}
                    className={cn(
                      "flex items-center gap-3 rounded-lg border p-3",
                      r.missed && "border-destructive/40 bg-destructive/10",
                    )}
                  >
                    <Icon className={cn("h-5 w-5 shrink-0", r.missed ? "text-destructive" : "text-muted-foreground")} />
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-medium">{phoneFromJid(r.peer) || "Número desconhecido"}</p>
                      <p className="text-xs text-muted-foreground">
                        <span className={cn(r.missed && "font-semibold text-destructive")}>{label(r)}</span>
                        {" · "}
                        {when(r.startedAt)}
                        {r.agent ? ` · ${r.agent}` : ""}
                      </p>
                    </div>
                    {num && (
                      <Button
                        size="sm"
                        variant={r.missed ? "default" : "outline"}
                        disabled={startCall.isPending}
                        onClick={() =>
                          startCall.mutate({ phone: num, record: false, video: false }, { onSuccess: () => setOpen(false) })
                        }
                        aria-label={`Ligar para ${phoneFromJid(r.peer)}`}
                      >
                        <Phone className="h-4 w-4" />
                        Retornar
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
};
