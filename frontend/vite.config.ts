import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// ポートはdocs/api-contract.yamlのCORS想定（http://localhost:3000）に合わせて固定する。
// 変更する場合はバックエンド側のCORS許可オリジンも合わせて更新が必要。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3000,
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
});
