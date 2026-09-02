import { defineConfig } from "vite";
import preact from "@preact/preset-vite";

// The Go server embeds web/dist and serves it. In development, run `npm run dev`
// and start the server with `--dev-proxy http://localhost:5173` so the API and
// the live-reloading UI share one origin.
export default defineConfig({
  plugins: [preact()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2020",
    // Let Rollup split automatically: the dynamic import()s for `qrcode` and
    // `highlight.js` then land in their own chunks that only download when a QR
    // dialog or a highlighted snippet is first shown. `tus-js-client` is pulled
    // into its own chunk because the uploader (loaded eagerly) needs it.
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("tus-js-client")) return "tus";
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: "http://localhost:8787", changeOrigin: true, ws: false },
      "/s": { target: "http://localhost:8787", changeOrigin: true },
    },
  },
});
