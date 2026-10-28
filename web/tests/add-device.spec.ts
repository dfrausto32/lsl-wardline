import { expect, test, Page } from "@playwright/test";
import fs from "node:fs";
import { connectLabAgent, connectLabDevice, review, signIn } from "./helpers";
import { fmt, primary } from "../src/strings";

const a = primary.add;

type Kind = { platform: keyof typeof primary.platform; deviceName: string; phone: boolean; agent: boolean };
const KINDS: Kind[] = [
  { platform: "ios", deviceName: "Sam's iPhone", phone: true, agent: false },
  { platform: "android", deviceName: "Sam's Android phone", phone: true, agent: false },
  { platform: "windows", deviceName: "Sam's Windows computer", phone: false, agent: true },
  { platform: "macos", deviceName: "Sam's Mac", phone: false, agent: false },
  { platform: "linux", deviceName: "Sam's Linux computer", phone: false, agent: true },
];

async function startAdd(page: Page, person: string, kind: keyof typeof primary.platform) {
  await page.goto("/add");
  await expect(page.getByRole("heading", { name: a.whoseTitle })).toBeVisible();
  const existing = page.getByRole("button", { name: person, exact: true });
  if (await existing.count()) {
    await existing.click();
  } else {
    if (!(await page.getByLabel(a.newPersonLabel).count())) await page.getByRole("button", { name: a.someoneNew }).click();
    await page.getByLabel(a.newPersonLabel).fill(person);
    await page.getByRole("button", { name: primary.common.continue }).click();
  }
  await expect(page.getByRole("heading", { name: a.kindTitle })).toBeVisible();
  await page.getByRole("button", { name: primary.platform[kind] }).click();
}

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

for (const k of KINDS) {
  test(`add ${k.platform} (UX flow 2)`, async ({ page }) => {
    await startAdd(page, "Sam", k.platform);

    await expect(page.getByRole("heading", { name: k.deviceName })).toBeVisible();
    await expect(page.getByText(fmt(a.waiting, { name: k.deviceName }))).toBeVisible();

    if (k.agent) {
      // One command; the computer makes its own key. Linux needs no extra app; Windows needs WireGuard first.
      await expect(page.getByRole("heading", { name: a.agentTitle })).toBeVisible();
      const command = await page.locator("pre").first().innerText();
      expect(command).toContain(k.platform === "windows" ? "wardline.exe agent install" : "install-linux.sh agent");
      await expect(page.getByText(a.agentOnce)).toBeVisible();
      await expect(page.getByRole("button", { name: a.downloadButton })).toHaveCount(0);
      await expect(page.getByRole("img", { name: a.scanAlt })).toHaveCount(0);
      if (k.platform === "windows") {
        await expect(page.getByRole("heading", { name: a.installTitle })).toBeVisible();
        await expect(page.getByRole("link", { name: a.installButton })).toHaveAttribute("href", /wireguard/);
      } else {
        await expect(page.getByRole("heading", { name: a.installTitle })).toHaveCount(0);
      }
      await review(page, `add-${k.platform}`);
      return;
    }

    await expect(page.getByRole("heading", { name: a.installTitle })).toBeVisible();
    await expect(page.getByRole("link", { name: a.installButton })).toHaveAttribute("href", /wireguard|apple\.com|google\.com/);

    if (k.phone) {
      await expect(page.getByRole("heading", { name: a.scanTitle })).toBeVisible();
      const qr = page.getByRole("img", { name: a.scanAlt });
      await expect(qr).toBeVisible();
      expect(await qr.evaluate((img: HTMLImageElement) => img.naturalWidth > 100)).toBeTruthy();
      await expect(page.getByText(a.onceCode)).toBeVisible();
      await expect(page.getByRole("button", { name: a.downloadButton })).toHaveCount(0);
    } else {
      await expect(page.getByRole("heading", { name: a.downloadTitle })).toBeVisible();
      await expect(page.getByText(a.onceFile)).toBeVisible();
      await expect(page.getByRole("img", { name: a.scanAlt })).toHaveCount(0);
    }
    await review(page, `add-${k.platform}`);
  });
}

test("a computer set up with the agent connects, and the page updates by itself", async ({ page }) => {
  await startAdd(page, "Jo", "linux");
  await expect(page.getByRole("heading", { name: "Jo's Linux computer" })).toBeVisible();
  const command = await page.locator("pre").first().innerText();
  await expect(page.getByText(fmt(a.waiting, { name: "Jo's Linux computer" }))).toBeVisible();
  await connectLabAgent(page, "agent-1", command);
  await expect(page.getByText(fmt(a.connected, { name: "Jo's Linux computer" }))).toBeVisible({ timeout: 90_000 });
  await expect(page.getByRole("button", { name: primary.common.done })).toBeVisible();
  await review(page, "add-agent-connected");
});

test("a downloaded file connects a computer, and the page updates by itself", async ({ page }) => {
  await startAdd(page, "Sam", "macos");
  // A second Mac for the same person gets its own distinct name.
  await expect(page.getByRole("heading", { name: "Sam's Mac 2" })).toBeVisible();

  const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: a.downloadButton }).click()]);
  expect(download.suggestedFilename()).toBe("sams-mac-2.conf");
  const file = await download.path();
  const config = fs.readFileSync(file!, "utf8");
  expect(config).toContain("[Interface]");
  expect(config).toContain("Endpoint = server-a:51820");

  await expect(page.getByText(fmt(a.waiting, { name: "Sam's Mac 2" }))).toBeVisible();
  await connectLabDevice(page, "device-linux-1", config);

  // No refresh button: the waiting message turns into a green check on its own.
  await expect(page.getByText(fmt(a.connected, { name: "Sam's Mac 2" }))).toBeVisible({ timeout: 60_000 });
  await expect(page.getByRole("button", { name: primary.common.done })).toBeVisible();
  await expect(page.getByRole("button", { name: a.addAnother })).toBeVisible();
  await review(page, "add-connected");
});

test("a second person's computer connects too", async ({ page }) => {
  await startAdd(page, "Alex", "macos");
  await expect(page.getByRole("heading", { name: "Alex's Mac" })).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: a.downloadButton }).click()]);
  const config = fs.readFileSync((await download.path())!, "utf8");
  await connectLabDevice(page, "device-linux-2", config);
  await expect(page.getByText(fmt(a.connected, { name: "Alex's Mac" }))).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: primary.common.done }).click();
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();
});

test("the add button is always within reach", async ({ page }) => {
  await page.goto("/");
  await page.setViewportSize({ width: 375, height: 812 });
  const fab = page.getByRole("link", { name: primary.home.addDevice });
  await expect(fab).toBeVisible();
  const box = await fab.boundingBox();
  expect(box!.y + box!.height).toBeLessThanOrEqual(812);
  expect(box!.height).toBeGreaterThanOrEqual(44);
  await fab.click();
  await expect(page.getByRole("heading", { name: a.whoseTitle })).toBeVisible();
});
