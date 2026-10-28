import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { writeFileSync } from "node:fs";

// The built app is embedded into the Go binary from internal/webui/dist.
// Vite empties that folder on every build, so put the placeholder back: the Go
// build needs at least one file there even before the web app has been built.
const keepPlaceholder = () => ({
  name: "keep-placeholder",
  closeBundle() {
    writeFileSync("../internal/webui/dist/.gitkeep", "");
  },
});

export default defineConfig({
  plugins: [react(), keepPlaceholder()],
  build: { outDir: "../internal/webui/dist", emptyOutDir: true, sourcemap: false },
  server: { proxy: { "/api": "http://localhost:8080", "/gateway": "http://localhost:8080" } },
});
