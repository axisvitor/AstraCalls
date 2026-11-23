import { useEffect, useState } from "react";
import { Loader2, MessageSquare, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiGet, apiPut } from "@/lib/api";
import { useSessions } from "@/stores/sessions";
import { defaultBaseUrl } from "@/lib/auth";
import { phoneFromJid } from "@/utils/format";

type EmbedConfig = { inboxes: Record<string, string>; phoneAttrs: string[] };
type Row = { inbox: string; sid: string };

/** Admin: qual caixa de entrada do Chatwoot liga por qual linha, e onde está o número do contato. */
export const EmbedDialog = () => {
  const sessions = useSessions((s) => s.sessions);
  const [open, setOpen] = useState(false);
  const [rows, setRows] = useState<Row[]>([]);
  const [attrs, setAttrs] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    apiGet<EmbedConfig>("/api/embed/config")
      .then((c) => {
        setRows(Object.entries(c.inboxes ?? {}).map(([inbox, sid]) => ({ inbox, sid })));
        setAttrs((c.phoneAttrs ?? []).join(", "));
      })
      .catch((e) => toast.error((e as Error).message));
  }, [open]);

  const save = async () => {
    setBusy(true);
    try {
      const inboxes: Record<string, string> = {};
      for (const r of rows) if (r.inbox.trim() && r.sid) inboxes[r.inbox.trim()] = r.sid;
      const phoneAttrs = attrs.split(",").map((a) => a.trim()).filter(Boolean);
      await apiPut("/api/embed/config", { inboxes, phoneAttrs });
      toast.success("Configuração salva");
      setOpen(false);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const setRow = (i: number, patch: Partial<Row>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" title="Aba Ligações no Chatwoot">
          <MessageSquare className="h-4 w-4" />
          <span className="hidden sm:inline">Chatwoot</span>
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Aba Ligações no Chatwoot</DialogTitle>
          <DialogDescription>
            No Chatwoot, em Configurações → Integrações → Aplicativos do Painel, crie um aplicativo com a URL{" "}
            <code className="rounded bg-muted px-1">{defaultBaseUrl()}/embed/chatwoot</code>. Aqui você diz por qual
            linha cada caixa de entrada liga.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label>Caixa de entrada (ID no Chatwoot) → linha</Label>
          {rows.map((r, i) => (
            <div key={i} className="flex gap-2">
              <Input
                className="w-28"
                inputMode="numeric"
                placeholder="ID"
                aria-label="ID da caixa de entrada"
                value={r.inbox}
                onChange={(e) => setRow(i, { inbox: e.target.value.replace(/\D/g, "") })}
              />
              <select
                className="h-9 min-w-0 flex-1 rounded-full border border-input bg-card px-3 text-sm"
                aria-label="Linha"
                value={r.sid}
                onChange={(e) => setRow(i, { sid: e.target.value })}
              >
                <option value="">Escolha a linha…</option>
                {sessions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} {phoneFromJid(s.jid || s.lastJid)}
                  </option>
                ))}
              </select>
              <Button variant="outline" size="icon" aria-label="Remover" onClick={() => setRows(rows.filter((_, j) => j !== i))}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ))}
          <Button variant="outline" size="sm" onClick={() => setRows([...rows, { inbox: "", sid: "" }])}>
            <Plus className="h-4 w-4" />
            Adicionar caixa
          </Button>
          <p className="text-xs text-muted-foreground">
            O ID aparece na URL do Chatwoot ao abrir a caixa (…/inboxes/<b>12</b>/…). Mapeamentos de
            WACALLS_EMBED_INBOXES também valem.
          </p>
        </div>
        <div className="space-y-1">
          <Label htmlFor="embed-attrs">Atributos do contato com o número (em ordem)</Label>
          <Input id="embed-attrs" value={attrs} onChange={(e) => setAttrs(e.target.value)} />
          <p className="text-xs text-muted-foreground">Usados quando o contato não tem telefone preenchido.</p>
        </div>
        <Button onClick={save} disabled={busy}>
          {busy && <Loader2 className="h-4 w-4 animate-spin" />}
          Salvar
        </Button>
      </DialogContent>
    </Dialog>
  );
};
