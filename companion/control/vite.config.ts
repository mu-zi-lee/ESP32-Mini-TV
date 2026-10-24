import { defineConfig } from "vite";

export default defineConfig({
  // Keep machine-local defaults in .env without baking them into source.
  // MINI_TV_WIFI_PASSWORD is only used to pre-fill the form during local dev.
  envPrefix: ["VITE_", "MINI_TV_"],
  server: {
    // The app loads this address during `mygo dev` (devUrl in mygo.json).
    port: 5173,
    strictPort: true,
    // The development app and the packaged builds are not the page's.
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
});
