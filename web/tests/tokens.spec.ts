import { expect, test } from "@playwright/test";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const appCSS = join(here, "..", "src", "app.css");

test.describe("tokens", () => {
  test("app.css gets colors from tokens.css", () => {
    const css = readFileSync(appCSS, "utf8");
    expect(css.trimStart()).toMatch(/^@import\s+url\("tokens\.css"\);/);

    const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
    const literals = withoutComments.match(/#[0-9a-fA-F]{3,8}\b|(?:rgb|hsl)a?\(/g) ?? [];
    expect(literals).toEqual([]);
  });
});
