import { create } from "zustand";
import { storage } from "@/lib/storage";

type Theme = "light" | "dark";
const initial: Theme =
  (storage.get("theme") as Theme | null) ??
  (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");

export const useTheme = create<{ theme: Theme; toggle: () => void }>((set) => ({
  theme: initial,
  toggle: () =>
    set((s) => {
      const next: Theme = s.theme === "dark" ? "light" : "dark";
      storage.set("theme", next);
      return { theme: next };
    }),
}));

useTheme.subscribe((s) => {
  document.documentElement.classList.toggle("dark", s.theme === "dark");
});
document.documentElement.classList.toggle("dark", useTheme.getState().theme === "dark");
