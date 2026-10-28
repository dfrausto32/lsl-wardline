import AxeBuilder from "@axe-core/playwright";
import { Page, expect } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { BANNED } from "./banned";

// Lab-only values. The setup spec creates this account through the UI.
export const EMAIL = "ui@lab.test";
export const PASSWORD = "lab ui passphrase 1234";
export const LAB_DIR = process.env.LAB_DIR ?? "/lab";
export const SHOTS = path.join("test-results", "screens");

const SIZES = [
  { name: "desktop", width: 1280, height: 800 },
  { name: "iphone", width: 375, height: 812 },
];

/**
 * Looks at a screen the way acceptance item A8 asks: screenshots at 1280px and
 * 375px wide, no sideways scrolling, no serious accessibility problems, and
 * none of the banned words in what people can read.
 */
export async function review(page: Page, name: string, opts: { skipAxe?: boolean } = {}) {
  fs.mkdirSync(SHOTS, { recursive: true });
  for (const s of SIZES) {
    await page.setViewportSize({ width: s.width, height: s.height });
    await page.waitForTimeout(150);
    await page.screenshot({ path: path.join(SHOTS, `${name}-${s.name}.png`), fullPage: true });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, `${name} scrolls sideways at ${s.width}px`).toBeLessThanOrEqual(0);
    const clipped = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLElement>("h1,h2,h3,p,button,a,label,span,li")]
        .filter((el) => el.offsetParent !== null && el.scrollWidth > el.clientWidth + 1 && getComputedStyle(el).overflowX !== "visible" && getComputedStyle(el).display !== "inline")
        .map((el) => el.textContent?.slice(0, 40)),
    );
    expect(clipped, `${name} has clipped text at ${s.width}px`).toEqual([]);
  }
  await page.setViewportSize({ width: 1280, height: 800 });

  if (!opts.skipAxe) {
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
    const bad = results.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
    expect(bad.map((v) => `${v.id}: ${v.help} (${v.nodes.length})`), `${name} accessibility`).toEqual([]);
  }
  await expectPlainLanguage(page, name);
}

/**
 * What is visible on screen (the collapsed Advanced drawer is hidden) must avoid the banned words.
 * Code blocks are skipped (literal text to paste into a terminal) and so is the Advanced drawer, where technical wording is allowed.
 */
export async function expectPlainLanguage(page: Page, name: string) {
  const text = await page.evaluate(() => {
    const copy = document.body.cloneNode(true) as HTMLElement;
    copy.querySelectorAll("pre, details.advanced").forEach((e) => e.remove());
    document.body.appendChild(copy);
    const t = copy.innerText;
    copy.remove();
    return t;
  });
  const hits = BANNED.filter((w) => new RegExp(`(^|[^a-z0-9])${w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}($|[^a-z0-9])`, "i").test(text));
  expect(hits, `${name}: banned words on screen`).toEqual([]);
}

export async function signIn(page: Page) {
  const res = await page.request.post("/api/session", { data: { email: EMAIL, password: PASSWORD }, headers: { "X-Wardline": "1" } });
  expect(res.ok()).toBeTruthy();
}

export function writeLab(name: string, content: string) {
  const tmp = path.join(LAB_DIR, `${name}.tmp`);
  fs.writeFileSync(tmp, content);
  fs.renameSync(tmp, path.join(LAB_DIR, name));
}

export function lastOK(dev: string): Date | null {
  try {
    const n = parseInt(fs.readFileSync(path.join(LAB_DIR, `${dev}.lastok`), "utf8").trim(), 10);
    return Number.isFinite(n) ? new Date(n * 1000) : null;
  } catch {
    return null;
  }
}

export async function serverTunnelIP(page: Page): Promise<string> {
  const r = await page.request.get("/api/network");
  const net = await r.json();
  const base = String(net.advanced.address_range).split("/")[0].split(".");
  base[3] = "1";
  return base.join(".");
}

/** Hands the setup code from the command on screen to one of the lab's computers that runs the real agent. */
export async function connectLabAgent(page: Page, dev: string, command: string) {
  const token = command.trim().split(/\s+/).pop();
  expect(token, "the setup command ends with the one-time code").toBeTruthy();
  writeLab(`${dev}.token`, token!);
  writeLab(`${dev}.target`, `${await serverTunnelIP(page)}\n`);
}

/** Hands a downloaded config file to one of the lab's simulated devices. */
export async function connectLabDevice(page: Page, dev: string, configText: string) {
  writeLab(`${dev}.conf`, configText);
  writeLab(`${dev}.target`, `${await serverTunnelIP(page)}\n`);
}
