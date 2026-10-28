import { expect, test } from "@playwright/test";
import fs from "node:fs";
import { review, signIn } from "./helpers";
import { primary } from "../src/strings";

const h = primary.home;

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

test("Home lists people and their devices with plain status (UX flow 3)", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();

  const sam = page.getByRole("region", { name: "Sam" }).or(page.locator("section.person", { hasText: "Sam's iPhone" }));
  await expect(sam).toContainText("Sam's iPhone");
  await expect(sam).toContainText("Sam's Android phone");
  await expect(sam).toContainText("Sam's Windows computer");
  await expect(sam).toContainText("Sam's Mac");

  // Devices that were never switched on are Waiting; the two lab computers are Connected.
  const iphone = page.getByRole("link", { name: /Sam's iPhone/ });
  await expect(iphone).toContainText(primary.status.waiting);
  await expect(iphone).toContainText(primary.status.never);
  await expect(page.getByRole("link", { name: /Alex's Linux computer/ })).toContainText(primary.status.connected, { timeout: 30_000 });

  // A phone that has not connected yet is never an alarm.
  await expect(page.getByRole("region", { name: h.attentionRegion })).toHaveCount(0);
  // The first-device card is gone once there are devices.
  await expect(page.getByRole("heading", { name: h.firstCardTitle })).toHaveCount(0);
  await expect(page.getByText(h.serverOnline)).toBeVisible();
  await review(page, "home");
});

test("device details show status, last seen and Advanced (UX flow 4)", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: /Sam's iPhone/ }).click();
  await expect(page.getByRole("heading", { name: "Sam's iPhone" })).toBeVisible();
  const d = primary.device;
  await expect(page.getByText(d.status, { exact: true })).toBeVisible();
  await expect(page.getByText(primary.status.waiting).first()).toBeVisible();
  await expect(page.getByText(d.owner)).toBeVisible();
  await expect(page.getByRole("button", { name: d.removeAccess })).toBeVisible();

  // Times are relative with the exact time on hover.
  const added = page.locator("dd", { hasText: primary.status.justNow }).first();
  expect(await added.getAttribute("title")).toBeTruthy();

  // Technical detail lives in a drawer that starts closed.
  const drawer = page.locator("details.advanced");
  await expect(drawer).not.toHaveAttribute("open", "");
  await expect(page.getByText(/^[A-Za-z0-9+/]{43}=$/)).toBeHidden();
  await review(page, "device-details");
  await drawer.locator("summary").click();
  await expect(page.getByText(/^[A-Za-z0-9+/]{43}=$/)).toBeVisible();
  await review(page, "device-details-advanced");
});

test("Activity is a plain-language timeline with filter and export (UX flow 8)", async ({ page }) => {
  await page.goto("/activity");
  await expect(page.getByRole("heading", { name: primary.activity.title })).toBeVisible();
  const list = page.locator("ol.timeline");
  await expect(list).toContainText("Laney Office");
  await expect(list).toContainText("added Sam's iPhone.");
  await expect(list).toContainText("Alex's Linux computer connected for the first time.");
  await expect(list).toContainText("Sam's Linux computer 2 connected for the first time.");
  await review(page, "activity");

  // Filter by a person.
  await page.getByLabel(primary.activity.filterLabel).selectOption({ label: "Alex" });
  await expect(list.locator("li").filter({ hasText: "Sam's iPhone" })).toHaveCount(0);
  await expect(list).toContainText("Alex's Linux computer");

  // Export.
  const link = page.getByRole("link", { name: primary.activity.exportCsv });
  await expect(link).toHaveAttribute("href", /\/api\/activity\.csv\?person_id=/);
  const [download] = await Promise.all([page.waitForEvent("download"), link.click()]);
  const csv = fs.readFileSync((await download.path())!, "utf8");
  expect(csv.split("\n")[0]).toBe("When,Who,What happened");
  expect(csv).toContain("Alex's Linux computer");
  expect(csv).not.toContain("Sam's iPhone");

  // Times are relative with the exact time on hover.
  const time = page.locator("time").first();
  expect(await time.getAttribute("title")).toBeTruthy();
  expect(await time.innerText()).toMatch(/just now|minute|hour|day/);
});

test("the server footer and menu work on a phone-sized screen", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/");
  await expect(page.getByText(h.serverOnline)).toBeVisible();
  await expect(page.getByRole("navigation", { name: primary.common.mainNav })).toBeVisible();
  await page.getByRole("link", { name: primary.nav.activity }).click();
  await expect(page.getByRole("heading", { name: primary.activity.title })).toBeVisible();
});
