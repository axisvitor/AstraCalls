/**
 * localStorage que não quebra a página: num iframe de outro site (aba Ligações do
 * Chatwoot) com cookies de terceiros bloqueados, o navegador LANÇA erro ao tocar no
 * localStorage. Aí guardamos só em memória (o login vale até recarregar a aba).
 */
const mem = new Map<string, string>();

export const storage = {
  get(key: string): string | null {
    try {
      return window.localStorage.getItem(key) ?? mem.get(key) ?? null;
    } catch {
      return mem.get(key) ?? null;
    }
  },
  set(key: string, value: string): void {
    mem.set(key, value);
    try {
      window.localStorage.setItem(key, value);
    } catch {
      /* só memória */
    }
  },
  remove(key: string): void {
    mem.delete(key);
    try {
      window.localStorage.removeItem(key);
    } catch {
      /* só memória */
    }
  },
};
