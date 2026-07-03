/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

const config = defineConfig({
  resolve: {
    tsconfigPaths: true,
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  plugins: [devtools(), tailwindcss(), tanstackStart(), viteReact()],
  test: {
    // Default to the `node` environment so existing pure-logic tests are
    // unaffected. Component/integration tests opt into a DOM by adding a
    // `// @vitest-environment jsdom` docblock at the top of the test file
    // (the least-disruptive approach — see src/test/setup.ts).
    environment: "node",
    // Shared setup: registers @testing-library/jest-dom matchers and, when a
    // DOM is present, installs a matchMedia shim + RTL auto-cleanup.
    setupFiles: ["./src/test/setup.ts"],
  },
});

export default config;
