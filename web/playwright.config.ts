import { defineConfig, devices } from "@playwright/test";

// measure.spec.ts times the app on emulated devices against 1000 and 5000
// issues and writes .b9s-e2e/measure/report/. It is listed only when it is
// asked for by name, so a plain run and CI never pay for it:
//   npx playwright test --project measure
// A worker process reads this file again without the command line, so the
// answer travels to it in the environment, which workers inherit.
const measureAsked = process.env.B9S_MEASURE === "1" || process.argv.some((a, n, all) => a === "--project=measure" || (a === "--project" && all[n + 1] === "measure"));
if (measureAsked) process.env.B9S_MEASURE = "1";

// Each test starts its own `b9s web` on a free port (tests/harness.ts), so
// there is no shared webServer. globalSetup builds the binary under test.
export default defineConfig({
  testDir: "tests",
  globalSetup: "./tests/global-setup.ts",
  timeout: 30000,
  expect: { timeout: 5000 },
  fullyParallel: true,
  workers: 4,
  reporter: [["list"]],
  // Reduced motion drops the app's transitions (app.css), so a tap never
  // lands on a sheet that is still sliding in. tests/motion.spec.ts says why.
  use: { trace: "retain-on-failure", actionTimeout: 5000, navigationTimeout: 10000, reducedMotion: "reduce" },
  // Phones get the one-column board; wide.spec.ts and keys.spec.ts cover
  // laptops and iPads, which have a keyboard. The desktop browsers also run
  // the reading and writing specs. detail.spec.ts measures the phone's
  // bottom sheet, which a desktop shows as a side panel instead.
  //
  // perf.spec.ts times a render against a budget, so it runs in projects of
  // its own that start once every other project has finished. Timed beside
  // three other workers on a four-core CI runner, the same render misses the
  // budget.
  projects: [
    { name: "pixel", use: { ...devices["Pixel 7"] }, testIgnore: /(wide|keys|perf|measure)\.spec/ },
    { name: "iphone", use: { ...devices["iPhone 14"] }, testIgnore: /(wide|keys|perf|measure)\.spec/ },
    ...(["Desktop Chrome", "Desktop Safari", "Desktop Firefox"] as const).map((device) => ({
      name: device === "Desktop Chrome" ? "desktop" : device.toLowerCase().replace(" ", "-"),
      use: { ...devices[device], viewport: { width: 1440, height: 900 } },
      testMatch: /(wide|keys|read|write|memory)\.spec/,
    })),
    { name: "ipad", use: { ...devices["iPad Pro 11"] }, testMatch: /(wide|keys)\.spec/ },
    ...(["Pixel 7", "iPhone 14"] as const).map((device) => ({
      name: device === "Pixel 7" ? "pixel-perf" : "iphone-perf",
      use: { ...devices[device] },
      testMatch: /perf\.spec/,
      dependencies: ["pixel", "iphone", "desktop", "desktop-safari", "desktop-firefox", "ipad"],
    })),
    // The spec launches the browsers and devices it measures itself; one
    // worker at a time keeps the timings from sharing the CPU. Its output
    // lives beside the e2e binary, where a plain run in the same checkout
    // neither cleans it nor shares its artifacts, and it keeps no Playwright
    // trace: the spec records its own.
    ...(measureAsked ? [{ name: "measure", testMatch: /measure\.spec/, fullyParallel: false, timeout: 20 * 60 * 1000, outputDir: "../.b9s-e2e/measure", use: { ...devices["Desktop Chrome"], trace: "off" as const } }] : []),
  ],
});
