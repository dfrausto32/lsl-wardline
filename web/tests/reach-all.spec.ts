import { expect, test } from "@playwright/test";
import { review, signIn } from "./helpers";
import { fmt, primary } from "../src/strings";

const a = primary.add;
const r = primary.reachAll;

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

test("an operator lets one machine reach all the others, is asked first, and can take it back (task 6.9)", async ({ page }) => {
  // Make a machine to give it to.
  await page.goto("/add");
  await page.getByRole("button", { name: primary.platform.macos }).click();
  await page.getByLabel(a.nameLabel).fill("Reach Test Mac");
  await page.getByRole("button", { name: primary.common.continue }).click();
  await expect(page.getByRole("heading", { name: "Reach Test Mac" })).toBeVisible();
  await page.goto("/");
  await page.getByRole("link", { name: /^Reach Test Mac/ }).first().click();
  await expect(page.getByRole("heading", { name: "Reach Test Mac" })).toBeVisible();
  const id = new URL(page.url()).pathname.split("/").pop()!;

  await page.locator("details.advanced > summary").click();
  const box = page.getByRole("checkbox", { name: r.label });
  await expect(box).not.toBeChecked();
  await expect(page.getByText(r.help)).toBeVisible();

  // It asks first, and says what it opens. Cancelling changes nothing.
  await box.click();
  const dialog = page.getByRole("dialog", { name: fmt(r.confirmTitle, { name: "Reach Test Mac" }) });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(fmt(r.confirmBody, { name: "Reach Test Mac" }))).toBeVisible();
  await review(page, "reach-all-confirm");
  await dialog.getByRole("button", { name: primary.common.cancel }).click();
  await expect(box).not.toBeChecked();
  expect((await (await page.request.get(`/api/devices/${id}`)).json()).can_reach_all).toBe(false);

  // Confirming turns it on, and it is still on after a reload.
  await box.click();
  await dialog.getByRole("button", { name: r.confirmOn }).click();
  await expect(page.getByText(fmt(r.savedOn, { name: "Reach Test Mac" }))).toBeVisible();
  await expect(box).toBeChecked();
  expect((await (await page.request.get(`/api/devices/${id}`)).json()).can_reach_all).toBe(true);
  await page.reload();
  await page.locator("details.advanced > summary").click();
  await expect(page.getByRole("checkbox", { name: r.label })).toBeChecked();
  await review(page, "reach-all-on");

  // The network setting is not touched.
  const net = await (await page.request.get("/api/network")).json();
  expect(net.devices_reach_each_other).toBe(false);

  // Taking it away needs no question.
  await page.getByRole("checkbox", { name: r.label }).click();
  await expect(page.getByText(fmt(r.savedOff, { name: "Reach Test Mac" }))).toBeVisible();
  await expect(page.getByRole("checkbox", { name: r.label })).not.toBeChecked();
  expect((await (await page.request.get(`/api/devices/${id}`)).json()).can_reach_all).toBe(false);
});

test("Home points to the one-machine switch from the network setting", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText(primary.setup.reachOneMachine)).toBeVisible();
});
