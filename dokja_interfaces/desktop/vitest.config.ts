import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Screen tests only; the logic tests run with the Node test runner (see package.json).
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["tests/ui/**/*.test.{ts,tsx}"],
    setupFiles: ["./tests/ui/setup.ts"],
  },
});
