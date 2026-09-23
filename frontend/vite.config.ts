import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import wails from "@wailsio/runtime/plugins/vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9270,
    strictPort: true,
  },
  plugins: [svelte(), wails("./bindings")],
  // Monaco is large; it is served locally by the desktop app, so no need to warn.
  build: { chunkSizeWarningLimit: 5000 },
});
