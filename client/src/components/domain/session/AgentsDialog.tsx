import { useEffect, useState } from "react";
import { KeyRound, Loader2, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { useSessions } from "@/stores/sessions";
import { createAgent, deleteAgent, listAgents, updateAgent } from "@/services/agents";
import { phoneFromJid } from "@/utils/format";
import type { AgentInfo } from "@/stores/auth";

// Mensagem legível de um erro da API ("... 400 {"error":"..."}").
const errText = (e: unknown): string => {
  const m = (e as Error).message ?? String(e);
  const j = m.indexOf("{");
  if (j >= 0) {
    try {
      return (JSON.parse(m.slice(j)) as { error?: string }).error ?? m;
    } catch {
      /* texto puro */
    }
  }
  return m;
};

const SessionChecks = ({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) => {
  const sessions = useSessions((s) => s.sessions);
  if (sessions.length === 0) return <p className="text-xs text-muted-foreground">Nenhuma linha criada ainda.</p>;
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1">
      {sessions.map((s) => (
        <label key={s.id} className="flex items-center gap-1.5 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-primary"
            checked={value.includes(s.id)}
            onChange={(e) => onChange(e.target.checked ? [...value, s.id] : value.filter((x) => x !== s.id))}
          />
          {s.name}
          {phoneFromJid(s.jid || s.lastJid) && (
            <span className="text-muted-foreground">({phoneFromJid(s.jid || s.lastJid)})</span>
          )}
        </label>
      ))}
    </div>
  );
};

/** Gestão de atendentes (só administrador): criar, liberar linhas, trocar senha, remover. */
export const AgentsDialog = () => {
  const [open, setOpen] = useState(false);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: "", username: "", password: "", sessions: [] as string[] });
  const [toDelete, setToDelete] = useState<AgentInfo | null>(null);
  const [pwFor, setPwFor] = useState<string | null>(null);
  const [newPw, setNewPw] = useState("");

  const reload = () =>
    listAgents()
      .then(setAgents)
      .catch((e) => toast.error(errText(e)));

  useEffect(() => {
    if (open) void reload();
  }, [open]);

  const create = async () => {
    setBusy(true);
    try {
      await createAgent(form);
      toast.success("Atendente criado");
      setForm({ name: "", username: "", password: "", sessions: [] });
      await reload();
    } catch (e) {
      toast.error(errText(e));
    } finally {
      setBusy(false);
    }
  };

  const patch = async (id: string, body: Parameters<typeof updateAgent>[1], ok?: string) => {
    try {
      await updateAgent(id, body);
      if (ok) toast.success(ok);
      await reload();
    } catch (e) {
      toast.error(errText(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" title="Atendentes">
          <Users className="h-4 w-4" />
          <span className="hidden sm:inline">Atendentes</span>
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Atendentes</DialogTitle>
          <DialogDescription>
            Quem entra com usuário e senha só vê as linhas marcadas e só pode ligar, atender, pôr em espera, transferir e
            ver o histórico.
          </DialogDescription>
        </DialogHeader>

        <ul className="space-y-3">
          {agents.map((a) => (
            <li key={a.id} className="space-y-2 rounded-xl border p-3">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate font-medium">{a.name}</p>
                  <p className="truncate text-xs text-muted-foreground">usuário: {a.username}</p>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button variant="outline" size="sm" onClick={() => { setPwFor(pwFor === a.id ? null : a.id); setNewPw(""); }}>
                    <KeyRound className="h-4 w-4" />
                    Senha
                  </Button>
                  <Button variant="outline" size="icon" aria-label={`Remover ${a.name}`} onClick={() => setToDelete(a)}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
              {pwFor === a.id && (
                <form
                  className="flex gap-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    void patch(a.id, { password: newPw }, "Senha trocada").then(() => setPwFor(null));
                  }}
                >
                  <Input
                    type="password"
                    placeholder="nova senha (mín. 8)"
                    value={newPw}
                    autoComplete="new-password"
                    onChange={(e) => setNewPw(e.target.value)}
                  />
                  <Button type="submit" size="sm" disabled={newPw.length < 8}>
                    Salvar
                  </Button>
                </form>
              )}
              <SessionChecks value={a.sessions ?? []} onChange={(v) => void patch(a.id, { sessions: v })} />
            </li>
          ))}
          {agents.length === 0 && <p className="text-sm text-muted-foreground">Nenhum atendente ainda.</p>}
        </ul>

        <form
          className="space-y-3 rounded-xl border border-dashed p-3"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <p className="text-sm font-medium">Novo atendente</p>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor="ag-name">Nome</Label>
              <Input id="ag-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="space-y-1">
              <Label htmlFor="ag-user">Usuário</Label>
              <Input
                id="ag-user"
                value={form.username}
                autoCapitalize="none"
                spellCheck={false}
                onChange={(e) => setForm({ ...form, username: e.target.value.toLowerCase() })}
              />
            </div>
          </div>
          <div className="space-y-1">
            <Label htmlFor="ag-pw">Senha (mín. 8 caracteres)</Label>
            <Input
              id="ag-pw"
              type="password"
              autoComplete="new-password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
            />
          </div>
          <div className="space-y-1">
            <Label>Linhas liberadas</Label>
            <SessionChecks value={form.sessions} onChange={(v) => setForm({ ...form, sessions: v })} />
          </div>
          <Button type="submit" disabled={busy || !form.username.trim() || form.password.length < 8}>
            {busy && <Loader2 className="h-4 w-4 animate-spin" />}
            Criar atendente
          </Button>
        </form>

        <ConfirmDialog
          open={!!toDelete}
          onOpenChange={(o) => !o && setToDelete(null)}
          title="Remover atendente?"
          description={toDelete ? `${toDelete.name} perde o acesso na hora.` : undefined}
          confirmLabel="Remover"
          destructive
          onConfirm={() => {
            if (toDelete)
              void deleteAgent(toDelete.id)
                .then(reload)
                .catch((e) => toast.error(errText(e)));
          }}
        />
      </DialogContent>
    </Dialog>
  );
};
