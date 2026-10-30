set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/bin/packages"

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c default.tabularium.sha256
else
  shasum -a 256 -c default.tabularium.sha256
fi
cmp default.tabularium "$root/.tabularium"

for label in linux-x64 linux-arm64 darwin-x64 darwin-arm64 win-x64 windows-aarch64; do
  archive="opensearch-plugin-$label.zip"
  test -f "$archive"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$archive.sha256"
  else
    shasum -a 256 -c "$archive.sha256"
  fi
  unzip -tq "$archive"
  unzip -p "$archive" .tabularium | jq -e '.id == "opensearch" and .capabilities.readonly == true and .license == "Apache-2.0" and .color == "#737373" and .icon == "https://raw.githubusercontent.com/opensearch-project/project-website/0823cd8d19351def7f000f729b05b6a5f953e5cb/assets/brand/SVG/Mark/opensearch_mark_default.svg"' >/dev/null
  unzip -p "$archive" ui/dist/index.js >/dev/null
  unzip -p "$archive" ui/dist/raw-source.js >/dev/null
  unzip -p "$archive" ppl.bundle.js | shasum -a 256 | grep -F "$(cut -d' ' -f1 "$root/ppl/bundle.sha256")" >/dev/null
  unzip -p "$archive" LICENSE >/dev/null
  unzip -p "$archive" NOTICE >/dev/null
  unzip -p "$archive" licenses/ppl/opensearch-dashboards-LICENSE.txt >/dev/null
  unzip -p "$archive" licenses/ppl/antlr4ng-LICENSE.txt >/dev/null
  unzip -p "$archive" licenses/ppl/goja-LICENSE.txt >/dev/null
  unzip -p "$archive" licenses/go/github.com/dop251/goja/ftoa/LICENSE_LUCENE >/dev/null
  unzip -p "$archive" licenses/go/github.com/dop251/goja/ftoa/internal/fast/LICENSE_V8 >/dev/null
  unzip -p "$archive" licenses/go-stdlib/LICENSE >/dev/null
  unzip -p "$archive" locales/en.json | jq -e . >/dev/null
  unzip -p "$archive" docs/specification.md >/dev/null
  unzip -p "$archive" docs/implementation-plan.md >/dev/null
  unzip -p "$archive" docs/compatibility.md >/dev/null
  unzip -p "$archive" docs/tickets/001-opensearch-driver-v0.1.0.md >/dev/null
  unzip -p "$archive" tests/host/README.md >/dev/null
done

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) label=linux-x64 ;;
  Linux-aarch64) label=linux-arm64 ;;
  Darwin-x86_64) label=darwin-x64 ;;
  Darwin-arm64) label=darwin-arm64 ;;
  *) echo "No native smoke target for this runner" >&2; exit 1 ;;
esac
scratch=$(mktemp -d "$root/bin/smoke.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
unzip -p "opensearch-plugin-$label.zip" opensearch-plugin > "$scratch/opensearch-plugin"
unzip -p "opensearch-plugin-$label.zip" ppl.bundle.js > "$scratch/ppl.bundle.js"
chmod +x "$scratch/opensearch-plugin"
printf '%s\n' '{"jsonrpc":"2.0","method":"get_databases","id":1}' | \
  "$scratch/opensearch-plugin" | jq -e '.id == 1 and .result == ["default"]' >/dev/null
printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"execute_query","params":{"params":{"extra":{"endpoint":"http://127.0.0.1:1","deployment":"self_hosted","auth_mode":"anonymous","request_timeout_ms":"60000"}},"query":"#!ppl\nsource=events | fields count","limit":10,"page":1}}' | \
  "$scratch/opensearch-plugin" | jq -e '.id == 2 and (.error.message | contains("OpenSearch transport failed"))' >/dev/null
