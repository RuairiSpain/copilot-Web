// One-off dev utility: rasterizes public/icons/icon.svg into the PNG sizes
// the manifest references (public/icons/icon-192.png, icon-512.png) plus an
// iOS apple-touch-icon. Run with `node scripts/generate-icons.mjs` after
// editing the source SVG. Not part of the app's runtime — `sharp` is only a
// transitive dependency here (pulled in by Next.js), not something the app
// itself imports.
import sharp from "sharp";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const dir = path.dirname(fileURLToPath(import.meta.url));
const svgPath = path.join(dir, "../public/icons/icon.svg");
const svg = readFileSync(svgPath);

const targets = [
    { size: 192, out: "icon-192.png" },
    { size: 512, out: "icon-512.png" },
    { size: 180, out: "apple-touch-icon.png" },
];

for (const { size, out } of targets) {
    const outPath = path.join(dir, "../public/icons", out);
    await sharp(svg, { density: 384 }).resize(size, size).png().toFile(outPath);
    console.log(`wrote ${outPath}`);
}
