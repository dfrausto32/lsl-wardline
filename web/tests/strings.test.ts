import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { advanced, primary } from "../src/strings";

import { BANNED } from "./banned";

const re = (w: string) => new RegExp(`(^|[^a-z0-9])${w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}($|[^a-z0-9])`, "i");

function leaves(obj: unknown, path = ""): [string, string][] {
  if (typeof obj === "string") return [[path, obj]];
  if (obj && typeof obj === "object") {
    return Object.entries(obj).flatMap(([k, v]) => leaves(v, path ? `${path}.${k}` : k));
  }
  return [];
}

describe("plain-language strings", () => {
  const strings = leaves(primary);

  it("has strings to check", () => {
    expect(strings.length).toBeGreaterThan(50);
  });

  it("uses none of the banned words outside Advanced", () => {
    const hits = strings.flatMap(([path, text]) => BANNED.filter((w) => re(w).test(text)).map((w) => `${path}: "${w}" in "${text}"`));
    expect(hits).toEqual([]);
  });

  it("has no exclamation marks and no raw error vocabulary", () => {
    const hits = strings.filter(([, t]) => t.includes("!") || /\b(exception|stack ?trace|status code|undefined|null|NaN)\b/i.test(t));
    expect(hits).toEqual([]);
  });

  it("keeps technical wording in the Advanced namespace", () => {
    const adv = leaves(advanced);
    expect(adv.length).toBeGreaterThan(3);
  });
});

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    return statSync(p).isDirectory() ? sourceFiles(p) : p.endsWith(".tsx") ? [p] : [];
  });
}

// Words people read on screen can also be typed straight into a component. Catch that too.
function visibleLiterals(src: string): string[] {
  const noComments = src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  const out: string[] = [];
  for (const m of noComments.matchAll(/>([^<>{}]+)</g)) out.push(m[1]);
  for (const m of noComments.matchAll(/(?:aria-label|title|placeholder|alt)=["']([^"']+)["']/g)) out.push(m[1]);
  return out.map((s) => s.trim()).filter(Boolean);
}

describe("components do not hard-code banned words", () => {
  for (const file of sourceFiles(join(__dirname, "..", "src"))) {
    it(file.split(/[\\/]src[\\/]/)[1], () => {
      const hits = visibleLiterals(readFileSync(file, "utf8")).flatMap((t) => BANNED.filter((w) => re(w).test(t)).map((w) => `"${w}" in "${t}"`));
      expect(hits).toEqual([]);
    });
  }
});
