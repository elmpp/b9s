#!/usr/bin/env node
import { mkdir } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const require = createRequire(new URL("../../../web/package.json", import.meta.url));
const { chromium } = require("@playwright/test");

const here = dirname(fileURLToPath(import.meta.url));
const htmlUrl = pathToFileURL(join(here, "index.html")).href;
const shotsDir = join(here, "shots");
const minChangedRatio = 0.05;

const directions = ["a", "b", "c"];
const screens = [
  { id: "phone-tree", width: 390, height: 780 },
  { id: "phone-detail", width: 390, height: 780 },
  { id: "phone-board", width: 390, height: 780 },
  { id: "phone-search", width: 390, height: 780 },
  { id: "phone-edit", width: 390, height: 780 },
  { id: "tablet-portrait", width: 820, height: 1080 },
  { id: "tablet-landscape", width: 1180, height: 820 },
  { id: "desktop", width: 1440, height: 900 },
  { id: "wide", width: 2560, height: 960 },
];

const pairs = [
  ["a", "b"],
  ["a", "c"],
  ["b", "c"],
];

function dataUrl(buffer) {
  return `data:image/png;base64,${buffer.toString("base64")}`;
}

async function pixelDiffRatio(page, left, right) {
  return page.evaluate(async ({ leftUrl, rightUrl }) => {
    async function loadImage(url) {
      return new Promise((resolve, reject) => {
        const image = new Image();
        image.onload = () => resolve(image);
        image.onerror = () => reject(new Error(`could not load ${url.slice(0, 40)}`));
        image.src = url;
      });
    }

    const [leftImage, rightImage] = await Promise.all([
      loadImage(leftUrl),
      loadImage(rightUrl),
    ]);
    if (leftImage.naturalWidth !== rightImage.naturalWidth || leftImage.naturalHeight !== rightImage.naturalHeight) {
      throw new Error(`size mismatch ${leftImage.naturalWidth}x${leftImage.naturalHeight} vs ${rightImage.naturalWidth}x${rightImage.naturalHeight}`);
    }

    const canvas = document.createElement("canvas");
    canvas.width = leftImage.naturalWidth;
    canvas.height = leftImage.naturalHeight;
    const context = canvas.getContext("2d", { willReadFrequently: true });

    context.drawImage(leftImage, 0, 0);
    const leftPixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    context.clearRect(0, 0, canvas.width, canvas.height);
    context.drawImage(rightImage, 0, 0);
    const rightPixels = context.getImageData(0, 0, canvas.width, canvas.height).data;

    let changed = 0;
    for (let offset = 0; offset < leftPixels.length; offset += 4) {
      const delta =
        Math.abs(leftPixels[offset] - rightPixels[offset]) +
        Math.abs(leftPixels[offset + 1] - rightPixels[offset + 1]) +
        Math.abs(leftPixels[offset + 2] - rightPixels[offset + 2]) +
        Math.abs(leftPixels[offset + 3] - rightPixels[offset + 3]);
      if (delta > 40) changed += 1;
    }
    return changed / (leftPixels.length / 4);
  }, { leftUrl: dataUrl(left), rightUrl: dataUrl(right) });
}

await mkdir(shotsDir, { recursive: true });

const browser = await chromium.launch();
const renderPage = await browser.newPage();
const comparePage = await browser.newPage();
const failures = [];

for (const screen of screens) {
  const captures = new Map();
  for (const direction of directions) {
    await renderPage.setViewportSize({
      width: Math.max(screen.width + 80, 900),
      height: Math.max(screen.height + 180, 900),
    });
    await renderPage.goto("about:blank");
    await renderPage.goto(`${htmlUrl}#${screen.id}`);
    await renderPage.locator(`#directionControls [data-direction="${direction}"]`).click();
    await renderPage.evaluate(() => {
      const lab = document.querySelector(".lab");
      if (lab) lab.style.display = "none";
      const preview = document.querySelector(".preview");
      if (preview) preview.style.padding = "0";
    });
    const device = renderPage.locator(".device");
    await device.waitFor({ state: "visible" });
    const path = join(shotsDir, `${direction}-${screen.id}.png`);
    const buffer = await device.screenshot({ path, animations: "disabled" });
    captures.set(direction, buffer);
  }

  for (const [left, right] of pairs) {
    const ratio = await pixelDiffRatio(comparePage, captures.get(left), captures.get(right));
    const pct = (ratio * 100).toFixed(2);
    console.log(`${screen.id} ${left}/${right}: ${pct}% changed pixels`);
    if (ratio < minChangedRatio) {
      failures.push(`${screen.id} ${left}/${right}: ${pct}%`);
    }
  }
}

await browser.close();

if (failures.length > 0) {
  console.error(`Direction checks failed. Expected at least ${(minChangedRatio * 100).toFixed(0)}% changed pixels.`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`PASS ${screens.length * directions.length} screenshots written to ${shotsDir}`);
