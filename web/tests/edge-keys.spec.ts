import { expect, test } from "@playwright/test";
import { review, signIn } from "./helpers";
import { fmt, primary } from "../src/strings";

const e = primary.edge;

test.beforeEach(async ({ page }) => {
  await signIn(page);
});

test("an operator makes a read-only key for the Edge, sees it once, and can remove it (task 6.8)", async ({ page }) => {
  await page.goto("/server");
  await page.locator("details.advanced > summary").first().click();
  await expect(page.getByRole("heading", { name: e.heading })).toBeVisible();
  await expect(page.getByText(e.help)).toBeVisible();

  await page.getByLabel(e.nameLabel).fill("Front door Edge");
  await page.getByRole("button", { name: e.make }).click();

  // The key is shown once, in a box that can be copied.
  await expect(page.getByText(e.shownOnce)).toBeVisible();
  const shown = page.getByRole("region", { name: e.heading }).locator("pre code").first();
  const key = (await shown.innerText()).trim();
  expect(key).toMatch(/^wdl_edge_[A-Za-z0-9_-]{40,}$/);
  await review(page, "edge-key-made");

  // The key really works against the read-only API, and nothing else.
  const ok = await page.request.get("/api/v1/edge/members", { headers: { Authorization: `Bearer ${key}` } });
  expect(ok.status()).toBe(200);
  const body = await ok.json();
  expect(body.version).toBe("v1");
  const other = await page.request.get("/api/devices", { headers: { Authorization: `Bearer ${key}` }, failOnStatusCode: false });
  // The page is signed in with a cookie, so ask without it to prove the key alone opens nothing else.
  const bare = await page.context().browser()!.newContext({ baseURL: page.url().replace(/\/server.*$/, "") });
  const bareOther = await bare.request.get("/api/devices", { headers: { Authorization: `Bearer ${key}` } });
  expect(bareOther.status()).toBe(401);
  await bare.close();
  void other;

  // After a reload the key is gone from view but still listed by name.
  await page.reload();
  await page.locator("details.advanced > summary").first().click();
  await expect(page.getByText(key)).toHaveCount(0);
  await expect(page.getByText("Front door Edge")).toBeVisible();

  await page.getByRole("button", { name: fmt(e.removeNamed, { name: "Front door Edge" }) }).click();
  await expect(page.getByText("Front door Edge")).toHaveCount(0);
  const gone = await page.context().browser()!.newContext({ baseURL: page.url().replace(/\/server.*$/, "") });
  expect((await gone.request.get("/api/v1/edge/members", { headers: { Authorization: `Bearer ${key}` } })).status()).toBe(401);
  await gone.close();
});
