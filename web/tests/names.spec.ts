import { expect, test } from "@playwright/test";
import { review, signIn } from "./helpers";
import { fmt, primary } from "../src/strings";

const a = primary.add;
const d = primary.device;
const adv = primary.deviceNames;

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

async function startAdd(page: import("@playwright/test").Page, name: string) {
  await page.goto("/add");
  await page.getByRole("button", { name: primary.platform.macos }).click();
  await expect(page.getByRole("heading", { name: a.nameTitle })).toBeVisible();
  await page.getByLabel(a.nameLabel).fill(name);
}

test("adding a machine says what name the others will find it by (UX flow 9)", async ({ page }) => {
  await startAdd(page, "Names Test Mac");
  await expect(page.getByText(fmt(a.namePreview, { name: "names-test-mac" }))).toBeVisible();
  await page.getByRole("button", { name: primary.common.continue }).click();
  await expect(page.getByRole("heading", { name: "Names Test Mac" })).toBeVisible();

  // A different display name that makes the same short name is numbered, and the person is told before they commit.
  await startAdd(page, "names test  MAC!");
  await expect(page.getByText(fmt(a.nameNumbered, { wanted: "names-test-mac", name: "names-test-mac-2" }))).toBeVisible();
});

test("with nothing typed, the line uses the machine's kind", async ({ page }) => {
  await page.goto("/add");
  await page.getByRole("button", { name: primary.platform.macos }).click();
  await expect(page.getByText(/Other machines will find it as|is taken, so it will be called/)).toBeVisible();
});

test("a machine's page shows its name on the network, and Advanced lets you change it and add extra names", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: /^Names Test Mac/ }).first().click();
  await expect(page.getByRole("heading", { name: "Names Test Mac" })).toBeVisible();
  await expect(page.getByRole("heading", { name: d.networkNameLabel })).toBeVisible();
  await expect(page.getByText("names-test-mac.wardline.internal", { exact: true })).toBeVisible();

  await page.locator("details.advanced > summary").click();
  await page.getByLabel(adv.extraNames).fill("files, Nas");
  await page.getByRole("button", { name: adv.save }).click();
  await expect(page.getByText(adv.saved)).toBeVisible();
  await page.reload();
  await page.locator("details.advanced > summary").click();
  await expect(page.getByLabel(adv.extraNames)).toHaveValue("files, nas");

  // A name another machine already has is refused in plain words, and nothing is lost.
  await page.getByLabel(adv.shortName).fill("server");
  await page.getByRole("button", { name: adv.save }).click();
  await expect(page.getByRole("alert")).toContainText(/kept for Wardline/i);

  // Clearing the short name hands it back to the machine's name.
  await page.getByLabel(adv.shortName).fill("");
  await page.getByLabel(adv.extraNames).fill("");
  await page.getByRole("button", { name: adv.save }).click();
  await expect(page.getByText(adv.saved)).toBeVisible();
  await review(page, "device-names");
});

test("renaming says the old name stops working", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: /^Names Test Mac/ }).first().click();
  await expect(page.getByText(d.renameNamesNote)).toBeVisible();
});
