import { expect, test } from "@playwright/test";
import { review } from "./helpers";
import { primary } from "../src/strings";

// Runs in the setup project, before setup.spec.ts finishes first-run setup (the file name sorts first).
const s = primary.setup;

test("first-run asks one plain question about the kind of group, and only Managed is open to choose (task 6.6)", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: s.welcomeTitle })).toBeVisible();

  const group = page.getByRole("radiogroup", { name: s.modeQuestion });
  await expect(group).toBeVisible();
  const managed = group.getByRole("radio", { name: new RegExp(s.modeManaged) });
  const open = group.getByRole("radio", { name: new RegExp(s.modeOpen) });
  await expect(managed).toBeChecked();
  await expect(managed).toBeEnabled();
  await expect(open).toBeDisabled();
  await expect(page.getByText(s.modeManagedHelp)).toBeVisible();
  await expect(page.getByText(s.modeOpenHelp)).toBeVisible();
  await expect(page.getByText(s.modeOpenSoon)).toBeVisible();
  await review(page, "setup-1-group-mode");

  // The one big button still carries on with the managed group chosen.
  await expect(page.getByRole("button", { name: primary.common.continue })).toBeVisible();
});
