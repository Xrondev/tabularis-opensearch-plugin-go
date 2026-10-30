set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p .omx
scratch=$(mktemp -d "$root/.omx/artifact-guard-test.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/packages" "$scratch/content"
for file in bin/packages/*; do ln "$file" "$scratch/packages/$(basename "$file")"; done
archive=opensearch-plugin-linux-x64.zip
rm "$scratch/packages/$archive" "$scratch/packages/SHA256SUMS"
cp "bin/packages/$archive" "$scratch/packages/$archive"
unzip -q "$scratch/packages/$archive" opensearch-plugin -d "$scratch/content"
token=$(printf '%s%s' 'ghp_' "$(openssl rand -hex 18)")
printf '\n%s\n' "$token" >> "$scratch/content/opensearch-plugin"
(cd "$scratch/content" && zip -q "$scratch/packages/$archive" opensearch-plugin)
assets=(default.tabularium opensearch-plugin-linux-x64.zip opensearch-plugin-linux-arm64.zip opensearch-plugin-darwin-x64.zip opensearch-plugin-darwin-arm64.zip opensearch-plugin-win-x64.zip opensearch-plugin-windows-aarch64.zip)
(cd "$scratch/packages" && shasum -a 256 "${assets[@]}") > "$scratch/packages/SHA256SUMS"
if bash scripts/scan-release-artifacts.sh packages "$scratch/packages" > "$scratch/result" 2>&1; then
  echo "Synthetic binary secret was not detected" >&2
  exit 1
fi
grep -Fq 'secret scan failed or detected sensitive content' "$scratch/result" || { echo "Scanner did not reach binary secret check" >&2; exit 1; }
if grep -Fq "$token" "$scratch/result"; then
  echo "Scanner exposed a synthetic secret" >&2
  exit 1
fi
if GOFLAGS=-invalid-flag bash scripts/scan-release-artifacts.sh ppl ppl/dist/ppl.bundle.js > "$scratch/result" 2>&1; then
  echo "Scanner operational failure was not detected" >&2
  exit 1
fi
grep -Fq 'scanner unavailable' "$scratch/result" || { echo "Scanner operational failure was not identified" >&2; exit 1; }
echo "Artifact guard failure-path checks passed"
