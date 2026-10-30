import { readdir } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import assert from "node:assert/strict";

const [classifierPath, directory] = process.argv.slice(2);
assert.ok(classifierPath && directory, "classifier and asset directory are required");
const { inferPlatformKey } = await import(pathToFileURL(classifierPath).href);
const assets = await readdir(directory);
const expected = new Map([
  ["linux-x64", "opensearch-plugin-linux-x64.zip"],
  ["linux-arm64", "opensearch-plugin-linux-arm64.zip"],
  ["darwin-x64", "opensearch-plugin-darwin-x64.zip"],
  ["darwin-arm64", "opensearch-plugin-darwin-arm64.zip"],
  ["win-x64", "opensearch-plugin-win-x64.zip"],
  ["win-arm64", "opensearch-plugin-windows-aarch64.zip"],
]);

assert.deepEqual(new Set(assets), new Set([...expected.values(), "default.tabularium", "SHA256SUMS"]));
const byPlatform = new Map();
for (const name of assets) {
  const platform = inferPlatformKey(name);
  if (!platform) continue;
  assert.ok(!byPlatform.has(platform), `ambiguous registry platform ${platform}`);
  byPlatform.set(platform, name);
}
assert.deepEqual(byPlatform, expected);
assert.equal(inferPlatformKey("SHA256SUMS"), null);
console.log("Registry release asset layout is unambiguous.");
