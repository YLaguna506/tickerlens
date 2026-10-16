// Lists every t("...") in src/ that has no Spanish entry in src/es.ts.
// Keys built at runtime (t(variable)) are not seen here; es.ts lists those by
// hand.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const src = join(dirname(fileURLToPath(import.meta.url)), "..", "src");
const files = [];
(function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(ts|tsx)$/.test(name) && name !== "es.ts") files.push(p);
  }
})(src);

const keys = new Map();
const call = /\bt\(\s*"((?:[^"\\]|\\.)*)"/g;
for (const f of files) {
  const text = readFileSync(f, "utf8");
  for (const m of text.matchAll(call)) {
    const key = JSON.parse(`"${m[1]}"`);
    if (!keys.has(key)) keys.set(key, f.slice(src.length + 1));
  }
}

const es = readFileSync(join(src, "es.ts"), "utf8");
const known = new Set();
for (const m of es.matchAll(/^\s*("(?:[^"\\]|\\.)*"|[A-Za-z_$][\w$]*)\s*:/gm)) {
  known.add(m[1].startsWith('"') ? JSON.parse(m[1]) : m[1]);
}

const missing = [...keys].filter(([k]) => !known.has(k));
if (missing.length) {
  console.error(`${missing.length} text(s) without a Spanish translation in src/es.ts:`);
  for (const [k, f] of missing) console.error(`  ${JSON.stringify(k)}  (${f})`);
  process.exit(1);
}
console.log(`i18n: all ${keys.size} texts have a Spanish translation.`);
