set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mode=${1:?Expected ppl or packages}
input=${2:?Artifact path is required}
mkdir -p "$root/.omx"
scratch=$(mktemp -d "$root/.omx/artifact-scan.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT

fail() { echo "Artifact security validation failed: $1" >&2; exit 1; }
mkdir "$scratch/tools" "$scratch/empty"
GOBIN="$scratch/tools" go install github.com/zricethezav/gitleaks/v8@v8.30.1 >/dev/null 2>&1 || fail "scanner unavailable"
scanner="$scratch/tools/gitleaks"
scan() {
  (cd "$scratch/empty" && env -u GITLEAKS_CONFIG -u GITLEAKS_CONFIG_TOML "$scanner" "$@" --no-banner --redact=100 --ignore-gitleaks-allow --gitleaks-ignore-path "$scratch/empty" >/dev/null 2>&1) || fail "secret scan failed or detected sensitive content"
}

case "$mode" in
  ppl)
    test -f "$input" && test ! -L "$input" || fail "invalid PPL bundle"
    expected=$(awk '{print $1}' ppl/bundle.sha256)
    actual=$(shasum -a 256 "$input" | awk '{print $1}')
    test "$actual" = "$expected" || fail "PPL bundle hash mismatch"
    scan stdin < "$input"
    ;;
  packages)
    test -d "$input" && test ! -L "$input" || fail "invalid package directory"
    input=$(cd "$input" && pwd)
    assets=(default.tabularium opensearch-plugin-linux-x64.zip opensearch-plugin-linux-arm64.zip opensearch-plugin-darwin-x64.zip opensearch-plugin-darwin-arm64.zip opensearch-plugin-win-x64.zip opensearch-plugin-windows-aarch64.zip)
    shopt -s nullglob dotglob
    entries=("$input"/*)
    test "${#entries[@]}" -eq 8 || fail "unexpected package inventory"
    for asset in "${assets[@]}" SHA256SUMS; do
      test -f "$input/$asset" && test ! -L "$input/$asset" || fail "missing or invalid package asset"
    done
    (cd "$input" && shasum -a 256 "${assets[@]}") > "$scratch/checksums"
    cmp -s "$scratch/checksums" "$input/SHA256SUMS" || fail "package checksum mismatch"
    go build -o "$scratch/tools/artifactguard" ./scripts/artifactguard || fail "archive validator unavailable"
    for i in "${!assets[@]}"; do
      asset=${assets[$i]}
      [[ "$asset" == *.zip ]] || continue
      destination="$scratch/extracted/$i"
      mkdir -p "$destination"
      "$scratch/tools/artifactguard" "$input/$asset" "$destination" "$scratch/inventory-$i" || fail "invalid archive"
      if test -f "$scratch/inventory"; then
        cmp -s "$scratch/inventory" "$scratch/inventory-$i" || fail "archive inventories differ"
      else
        cp "$scratch/inventory-$i" "$scratch/inventory"
      fi
      binary="$destination/opensearch-plugin"
      case "$asset" in opensearch-plugin-win-*|opensearch-plugin-windows-*) binary="$binary.exe" ;; esac
      test -f "$binary" || fail "archive binary missing"
      strings -a "$binary" | scan stdin
    done
    for required in .tabularium README.md LICENSE NOTICE opensearch-plugin ppl.bundle.js ui/dist/index.js ui/dist/raw-source.js docs/specification.md docs/implementation-plan.md docs/compatibility.md docs/tickets/001-opensearch-driver-v0.1.0.md tests/host/README.md; do
      grep -Fxq "$required" "$scratch/inventory" || fail "required archive file missing"
    done
    git ls-files -- 'locales/*.json' | LC_ALL=C sort > "$scratch/expected-locales"
    grep '^locales/' "$scratch/inventory" > "$scratch/actual-locales" || fail "locale files missing"
    cmp -s "$scratch/expected-locales" "$scratch/actual-locales" || fail "locale inventory mismatch"
    scan dir "$scratch/extracted"
    scan stdin < "$input/default.tabularium"
    ;;
  *) fail "unknown scan mode" ;;
esac
echo "Artifact security validation passed: $mode"
