import { expect, test } from "@playwright/test";
import { EMAIL, PASSWORD, review, signIn, writeLab } from "./helpers";
import { primary } from "../src/strings";

const s = primary.setup;

test("first-run setup, one screen at a time (UX flow 1)", async ({ page }) => {
  await page.goto("/");

  // 1. Welcome and name the network.
  await expect(page.getByRole("heading", { name: s.welcomeTitle })).toBeVisible();
  await expect(page.getByText(s.welcomeBody)).toBeVisible();
  await expect(page.getByLabel(s.nameLabel)).toHaveValue(s.nameDefault);
  await review(page, "setup-1-welcome");
  await page.getByLabel(s.nameLabel).fill("Laney Office");
  await page.getByRole("button", { name: primary.common.continue }).click();

  // 2. Create the sign-in. A weak password is explained in plain words.
  await expect(page.getByRole("heading", { name: s.signInTitle })).toBeVisible();
  await page.getByLabel(s.email).fill(EMAIL);
  await page.getByLabel(s.password).fill("short");
  await page.getByRole("button", { name: s.signInButton }).click();
  await review(page, "setup-2-signin");
  await page.getByLabel(s.password).fill(PASSWORD);
  await page.getByRole("button", { name: s.signInButton }).click();

  // 3. Optional extra check: shown, explained, and skippable.
  await expect(page.getByRole("heading", { name: s.twoStepTitle })).toBeVisible();
  await review(page, "setup-3-extra-check");
  await page.getByRole("button", { name: s.twoStepSetUp }).click();
  await expect(page.getByRole("img", { name: s.twoStepScan })).toBeVisible();
  await expect(page.getByLabel(s.twoStepEnter)).toBeVisible();
  await review(page, "setup-3b-authenticator");
  await page.getByRole("button", { name: s.twoStepSkip }).click();

  // 4. Where will the server live?
  await expect(page.getByRole("heading", { name: s.whereTitle })).toBeVisible();
  await review(page, "setup-4-where");
  await page.getByRole("button", { name: new RegExp(s.whereServer) }).click();

  // 5. The address. An IP address brings the warning; a name does not.
  await expect(page.getByRole("heading", { name: s.addressTitle })).toBeVisible();
  await page.getByLabel(s.addressLabel).fill("203.0.113.5");
  await expect(page.getByText(s.addressIpNote)).toBeVisible();
  await review(page, "setup-5-address-ip");
  await page.getByLabel(s.addressLabel).fill("127.0.0.1");
  await page.getByRole("button", { name: primary.common.continue }).click();
  await expect(page.getByRole("alert")).toBeVisible(); // an address that only works on the server itself is refused
  await page.getByLabel(s.addressLabel).fill("server-a");
  await expect(page.getByText(s.addressIpNote)).toBeHidden();
  await expect(page.getByText(s.addressRecommend)).toBeVisible();
  await page.getByRole("button", { name: primary.common.continue }).click();

  // 6. The command, then the live check.
  await expect(page.getByRole("heading", { name: s.commandTitle })).toBeVisible();
  await expect(page.getByText(s.waiting)).toBeVisible();
  const command = await page.locator("pre").first().innerText();
  const token = /WARDLINE_ENROLL_TOKEN=(\S+)/.exec(command)?.[1];
  expect(token, "the command carries the one-time code").toBeTruthy();
  await review(page, "setup-6-command-waiting");

  // The lab's server-a container is waiting for this code. Give it the code, as a person would paste the command.
  writeLab("token-server-a", token!);
  await expect(page.getByText(s.serverOnline)).toBeVisible({ timeout: 60_000 });
  await review(page, "setup-6-command-online");
  await page.getByRole("button", { name: s.goHome }).click();

  // 7. Home, with a single card: add the first device.
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();
  await expect(page.getByRole("heading", { name: primary.home.firstCardTitle })).toBeVisible();
  await expect(page.getByText(primary.home.serverOnline)).toBeVisible();
  await review(page, "home-first-run");
});

test("setup survives a reload and sign-in works after signing out", async ({ page }) => {
  await signIn(page);
  await page.goto("/");
  // Still signed in after a reload: no setup, no sign-in screen.
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();

  await page.getByRole("button", { name: primary.common.signOut }).click();
  await expect(page.getByRole("heading", { name: primary.signIn.title })).toBeVisible();
  await review(page, "sign-in");

  // Wrong password: a plain message, never a hint about which part was wrong.
  await page.getByLabel(primary.signIn.email).fill(EMAIL);
  await page.getByLabel(primary.signIn.password).fill("not the password");
  await page.getByRole("button", { name: primary.signIn.button }).click();
  await expect(page.getByRole("alert")).toContainText("do not match");

  await page.getByLabel(primary.signIn.password).fill(PASSWORD);
  await page.getByRole("button", { name: primary.signIn.button }).click();
  await expect(page.getByRole("heading", { name: "Laney Office" })).toBeVisible();
});

test("the app can be installed to a home screen", async ({ page, request }) => {
  await page.goto("/");
  const href = await page.locator('link[rel="manifest"]').getAttribute("href");
  expect(href).toBeTruthy();
  const manifest = await (await request.get(href!)).json();
  expect(manifest.display).toBe("standalone");
  expect(manifest.start_url).toBe("/");
  const sizes = manifest.icons.map((i: { sizes: string }) => i.sizes);
  expect(sizes).toEqual(expect.arrayContaining(["192x192", "512x512"]));
  for (const icon of manifest.icons) expect((await request.get(icon.src)).ok()).toBeTruthy();
  expect(await page.locator('link[rel="apple-touch-icon"]').count()).toBe(1);
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute("content", /width=device-width/);

  const registered = await page.evaluate(async () => {
    const reg = await navigator.serviceWorker.ready;
    return !!reg.active;
  });
  expect(registered, "a service worker is active, which browsers require for installing").toBeTruthy();
});
