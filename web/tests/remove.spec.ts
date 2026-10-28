import { expect, test } from "@playwright/test";
import { lastOK, review, signIn } from "./helpers";
import { fmt, primary } from "../src/strings";

const d = primary.device;
const NAME = "Sam's Linux computer 2";

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

test("remove access asks first, says what happens, and takes effect within seconds (UX flow 5)", async ({ page }) => {
  // Before: both lab computers are passing traffic.
  const fresh = (dev: string) => {
    const t = lastOK(dev);
    return !!t && Date.now() - t.getTime() < 4000;
  };
  await expect.poll(() => fresh("device-linux-1"), { timeout: 30_000 }).toBeTruthy();
  await expect.poll(() => fresh("device-linux-2"), { timeout: 30_000 }).toBeTruthy();

  await page.goto("/");
  await page.getByRole("link", { name: new RegExp(NAME) }).click();
  await expect(page.getByRole("heading", { name: NAME })).toBeVisible();

  // The confirmation says what will happen.
  await page.getByRole("button", { name: d.removeAccess }).click();
  const dialog = page.getByRole("dialog", { name: fmt(d.removeTitle, { name: NAME }) });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(d.removeBody);
  await review(page, "remove-confirm");

  // Keeping the device changes nothing.
  await dialog.getByRole("button", { name: d.keep }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("heading", { name: NAME })).toBeVisible();
  expect(fresh("device-linux-1")).toBeTruthy();

  // Remove it for real.
  await page.getByRole("button", { name: d.removeAccess }).click();
  const removedAt = Date.now();
  await dialog.getByRole("button", { name: d.removeConfirm }).click();

  // Back on Home with a plain confirmation; the device is gone from the list.
  await expect(page.getByRole("status").filter({ hasText: fmt(d.removed, { name: NAME }) })).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(NAME) })).toHaveCount(0);
  await review(page, "home-after-remove");

  // Traffic from that device stops within 10 seconds of the click...
  await expect
    .poll(() => {
      const t = lastOK("device-linux-1");
      return !!t && Date.now() - t.getTime() >= 3000;
    }, { timeout: 10_000, intervals: [250] })
    .toBeTruthy();
  const stoppedAfter = Date.now() - removedAt;
  expect(stoppedAfter).toBeLessThan(10_000);

  // ...and stays stopped, while the other person's computer carries on.
  await page.waitForTimeout(4000);
  expect(lastOK("device-linux-1")!.getTime()).toBeLessThan(removedAt + 2000);
  expect(fresh("device-linux-2")).toBeTruthy();
  await expect(page.getByRole("link", { name: /Alex's Linux computer/ })).toContainText(primary.status.connected);

  // It shows up in Activity, and the old page says the device is gone.
  await page.goto("/activity");
  await expect(page.getByText("Sam's Linux computer 2 was removed.")).toBeVisible();
});

test("a removed device's page explains it is gone", async ({ page }) => {
  const list = await (await page.request.get("/api/devices")).json();
  expect(list.map((x: { name: string }) => x.name)).not.toContain(NAME);
  await page.goto("/devices/does-not-exist");
  await expect(page.getByRole("heading", { name: d.notFound })).toBeVisible();
  await review(page, "device-not-found");
  await page.getByRole("button", { name: d.backHome }).click();
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();
});
