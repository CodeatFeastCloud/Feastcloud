// SPDX-License-Identifier: AGPL-3.0-only
import { readFile } from "node:fs/promises";

const lock = JSON.parse(await readFile(new URL("../package-lock.json", import.meta.url), "utf8"));
const allowed = new Set([
  "0BSD",
  "Apache-2.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "CC-BY-4.0",
  "ISC",
  "MIT",
  "MIT-0",
  "OFL-1.1"
]);

function hasReviewedBoundary(path, version, license) {
  return (
    license === "MPL-2.0" &&
    version === "1.33.0" &&
    /^node_modules\/lightningcss(?:-|$)/.test(path)
  );
}

const rejected = [];
const counts = new Map();

for (const [path, pkg] of Object.entries(lock.packages ?? {})) {
  if (!path.includes("node_modules/") || !pkg.version) continue;
  const license = pkg.license;
  counts.set(license ?? "(missing)", (counts.get(license ?? "(missing)") ?? 0) + 1);
  if (!license || (!allowed.has(license) && !hasReviewedBoundary(path, pkg.version, license))) {
    rejected.push({ path, version: pkg.version, license: license ?? "missing" });
  }
}

if (rejected.length > 0) {
  console.error("Unapproved or unknown dependency licenses:");
  for (const item of rejected) {
    console.error(`- ${item.path}@${item.version}: ${item.license}`);
  }
  process.exitCode = 1;
} else {
  const summary = [...counts.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([license, count]) => `${license}=${count}`)
    .join(", ");
  console.log(`All dependency licenses are approved (${summary}).`);
}
