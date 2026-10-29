// Builds every icon from the master logo. Run: pnpm run icons
//  - full logo for installs and large sizes (192, 512, maskable, Apple touch)
//  - shield only for tiny sizes (favicons), drawn from public/favicon.svg
import sharp from "sharp";
import { readFileSync, writeFileSync } from "node:fs";

const SRC = "assets/logo-source.png";
const { data, info } = await sharp(SRC).removeAlpha().raw().toBuffer({ resolveWithObject: true });
const px = (x, y) => [data[(y * info.width + x) * 3], data[(y * info.width + x) * 3 + 1], data[(y * info.width + x) * 3 + 2]];

// Find the red rounded square inside the black surround.
let minX = info.width, minY = info.height, maxX = 0, maxY = 0;
for (let y = 0; y < info.height; y++)
  for (let x = 0; x < info.width; x++) {
    const [r, g, b] = px(x, y);
    if (r > 200 && g < 60 && b < 60) {
      if (x < minX) minX = x;
      if (x > maxX) maxX = x;
      if (y < minY) minY = y;
      if (y > maxY) maxY = y;
    }
  }
const side = Math.min(maxX - minX + 1, maxY - minY + 1);
const [rr, gg, bb] = px(minX + 25, minY + Math.floor(side / 2));
const red = { r: rr, g: gg, b: bb };
console.log(`logo square ${side}px at ${minX},${minY}; red = rgb(${rr},${gg},${bb})`);

// Cut the logo out of the black surround and fill the corners with its red, so it can go edge to edge.
const inset = 16;
const mask = Buffer.from(`<svg width="${side}" height="${side}"><rect x="${inset}" y="${inset}" width="${side - 2 * inset}" height="${side - 2 * inset}" rx="${Math.round(side * 0.15)}" fill="#fff"/></svg>`);
const cut = await sharp(SRC).extract({ left: minX, top: minY, width: side, height: side }).composite([{ input: mask, blend: "dest-in" }]).png().toBuffer();
const bleed = await sharp({ create: { width: side, height: side, channels: 3, background: red } }).composite([{ input: cut }]).png().toBuffer();

const full = (n) => sharp(bleed).resize(n, n).png({ compressionLevel: 9 }).toBuffer();
writeFileSync("public/icon-192.png", await full(192));
writeFileSync("public/icon-512.png", await full(512));
writeFileSync("public/apple-touch-icon.png", await full(180));

// Maskable: the system may crop to a circle, so keep the artwork inside the safe zone.
const inner = await full(Math.round(512 * 0.74));
writeFileSync("public/icon-maskable-512.png", await sharp({ create: { width: 512, height: 512, channels: 3, background: red } }).composite([{ input: inner, gravity: "centre" }]).png({ compressionLevel: 9 }).toBuffer());

// Tiny sizes: just the shield.
const svg = readFileSync("public/favicon.svg");
writeFileSync("public/favicon-32.png", await sharp(svg, { density: 512 }).resize(32, 32).png().toBuffer());
writeFileSync("public/favicon-48.png", await sharp(svg, { density: 512 }).resize(48, 48).png().toBuffer());

// The Windows program icon (cmd/wardline/wardline.ico): the full logo with rounded, see-through corners when large, the shield when tiny.
const rounded = (n) => sharp(cut).resize(n, n).png({ compressionLevel: 9 }).toBuffer();
// Turn it into the resource the linker uses with: go run github.com/akavel/rsrc@v0.10.2 -ico cmd/wardline/wardline.ico -arch amd64 -o cmd/wardline/rsrc_windows_amd64.syso
const sizes = [
  [16, await sharp(svg, { density: 512 }).resize(16, 16).png().toBuffer()],
  [32, await sharp(svg, { density: 512 }).resize(32, 32).png().toBuffer()],
  [48, await sharp(svg, { density: 512 }).resize(48, 48).png().toBuffer()],
  [64, await rounded(64)],
  [128, await rounded(128)],
  [256, await rounded(256)],
];
const head = Buffer.alloc(6);
head.writeUInt16LE(1, 2); // type: icon
head.writeUInt16LE(sizes.length, 4);
let offset = 6 + 16 * sizes.length;
const entries = sizes.map(([n, png]) => {
  const e = Buffer.alloc(16);
  e[0] = n === 256 ? 0 : n; // 0 means 256
  e[1] = n === 256 ? 0 : n;
  e.writeUInt16LE(1, 4); // planes
  e.writeUInt16LE(32, 6); // bits per pixel
  e.writeUInt32LE(png.length, 8);
  e.writeUInt32LE(offset, 12);
  offset += png.length;
  return e;
});
writeFileSync("../cmd/wardline/wardline.ico", Buffer.concat([head, ...entries, ...sizes.map(([, p]) => p)]));

// The tray icons (cmd/wardline/tray-*.ico): the shield with a small colored badge for how things are.
// They are embedded in the program, so they must be rebuilt here whenever the shield changes.
function buildIco(list) {
  const h = Buffer.alloc(6);
  h.writeUInt16LE(1, 2);
  h.writeUInt16LE(list.length, 4);
  let off = 6 + 16 * list.length;
  const es = list.map(([n, png]) => {
    const e = Buffer.alloc(16);
    e[0] = n;
    e[1] = n;
    e.writeUInt16LE(1, 4);
    e.writeUInt16LE(32, 6);
    e.writeUInt32LE(png.length, 8);
    e.writeUInt32LE(off, 12);
    off += png.length;
    return e;
  });
  return Buffer.concat([h, ...es, ...list.map(([, p]) => p)]);
}
for (const [name, color] of [["connected", "#1f9d3a"], ["problem", "#e8a317"], ["off", "#8a8478"]]) {
  const badge = Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="47" cy="47" r="15" fill="${color}" stroke="#ffffff" stroke-width="4"/></svg>`);
  const list = [];
  for (const n of [16, 20, 24, 32, 48]) {
    const base = await sharp(svg, { density: 512 }).resize(n, n).png().toBuffer();
    const dot = await sharp(badge, { density: 512 }).resize(n, n).png().toBuffer();
    list.push([n, await sharp(base).composite([{ input: dot }]).png().toBuffer()]);
  }
  writeFileSync(`../cmd/wardline/tray-${name}.ico`, buildIco(list));
}
console.log("icons written");
