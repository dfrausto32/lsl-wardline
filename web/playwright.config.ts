import { defineConfig } from "@playwright/test";

// Tests run against the lab (see scripts/ui-test.ps1): a real control plane, a real
// server container, and real simulated devices. State carries from one spec to the next,
// so the setup project runs first and everything else runs in one worker, in order.
export default defineConfig({
  testDir: "tests",
  testMatch: "**/*.spec.ts",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [["list"]],
  outputDir: "test-results",
  use: {
    baseURL: process.env.BASE_URL ?? "http://localhost:8080",
    screenshot: "off",
    trace: "off",
  },
  projects: [
    { name: "setup", testMatch: "setup.spec.ts" },
    { name: "flows", testIgnore: "setup.spec.ts", dependencies: ["setup"] },
  ],
});
