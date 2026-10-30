set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/bin/packages"

for label in linux-x64 linux-arm64 darwin-x64 darwin-arm64 win-x64; do
  archive="opensearch-plugin-$label.zip"
  test -f "$archive"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$archive.sha256"
  else
    shasum -a 256 -c "$archive.sha256"
  fi
  unzip -tq "$archive"
  unzip -p "$archive" .tabularium | jq -e '.id == "opensearch" and .capabilities.readonly == true' >/dev/null
  unzip -p "$archive" ui/dist/index.js >/dev/null
  unzip -p "$archive" ui/dist/raw-source.js >/dev/null
  unzip -p "$archive" locales/en.json | jq -e . >/dev/null
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
chmod +x "$scratch/opensearch-plugin"
printf '%s\n' '{"jsonrpc":"2.0","method":"get_databases","id":1}' | \
  "$scratch/opensearch-plugin" | jq -e '.id == 1 and .result == ["default"]' >/dev/null
