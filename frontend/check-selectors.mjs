// Asserts every hardcoded selector in app.js matches the real ABI.
//
// This exists because a wrong selector is a SILENT failure: eth_call to a non-existent function
// returns "0x" rather than reverting, so the UI would just render zeros forever. Every read
// selector in the first draft of app.js was wrong, which is exactly why this check is here.
//
// Run: node check-selectors.mjs

import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";

// Uses foundry's `cast sig` rather than bundling a keccak implementation — cast is already a
// project dependency, and deriving the selector with the same tool the contracts are built with
// removes a whole class of "my JS keccak disagrees with solc" bugs.
const sigOf = (signature) =>
  execFileSync("cast", ["sig", signature], { encoding: "utf8" }).trim().toLowerCase();

const ABI = JSON.parse(
  readFileSync(new URL("./abi/index.js", import.meta.url), "utf8")
    .replace(/^.*?export const ABI = /s, "")
    .replace(/;\s*$/, "")
);

const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");

// Pull `name: "0xdeadbeef", // signature(args)` out of the SELECTORS block.
const block = app.match(/const SELECTORS = \{([\s\S]*?)\n\};/);
if (!block) {
  console.error("could not find SELECTORS block in app.js");
  process.exit(1);
}

const entries = [...block[1].matchAll(/(\w+):\s*"(0x[0-9a-fA-F]{8})",\s*\/\/\s*(\S+)/g)];
if (entries.length === 0) {
  console.error("no selectors parsed — is the comment format still `// signature(args)`?");
  process.exit(1);
}

// Build the set of signatures the ABIs actually expose.
const known = new Set();
for (const contract of Object.values(ABI)) {
  for (const item of contract) {
    if (item.type !== "function") continue;
    known.add(`${item.name}(${item.inputs.map((i) => i.type).join(",")})`);
  }
}

let failed = 0;
for (const [, key, selector, signature] of entries) {
  const expected = sigOf(signature);
  if (expected !== selector.toLowerCase()) {
    console.error(`FAIL ${key}: ${signature} => ${expected}, but app.js has ${selector}`);
    failed++;
  } else if (!known.has(signature)) {
    console.error(`FAIL ${key}: ${signature} is not present in any deployed ABI`);
    failed++;
  } else {
    console.log(`ok   ${key.padEnd(22)} ${selector}  ${signature}`);
  }
}

if (failed > 0) {
  console.error(`\n${failed} selector(s) wrong — the UI would silently read zeros.`);
  process.exit(1);
}
console.log(`\nall ${entries.length} selectors verified against the ABI`);
