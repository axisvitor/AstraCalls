import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  // caminhos relativos: o servidor injeta <base href> (prefixo do proxy, ex.:
  // /astracalls no domínio do Chatwoot), então o painel funciona sob um caminho.
  base: "./",
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:3001", changeOrigin: true, ws: false },
    },
  },
});
