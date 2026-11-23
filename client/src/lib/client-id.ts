import { storage } from "./storage";

const KEY = "wacalls.clientId";

const generate = (): string => {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return "c-" + Math.random().toString(36).slice(2) + Date.now().toString(36);
};

export const getClientId = (): string => {
  let id = storage.get(KEY);
  if (!id) {
    id = generate();
    storage.set(KEY, id);
  }
  return id;
};
