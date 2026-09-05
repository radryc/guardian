import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [tailwindcss()],
  root: resolve(__dirname, "internal/ui/static-src"),
  build: {
    outDir: resolve(__dirname, "internal/ui/static"),
    emptyOutDir: false,
    rollupOptions: {
      input: resolve(__dirname, "internal/ui/static-src/index.html"),
      output: {
        entryFileNames: "app.js",
        chunkFileNames: "app-[name].js",
        assetFileNames: (assetInfo) => {
          if (assetInfo.names?.some((n) => n.endsWith(".css"))) return "app.css";
          return "[name][extname]";
        },
      },
    },
  },
});
