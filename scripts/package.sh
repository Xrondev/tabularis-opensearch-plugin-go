set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
test -f ui/dist/index.js || { echo "Build the UI first: cd ui && npm ci && npm run build" >&2; exit 1; }
test -f ui/dist/raw-source.js || { echo "Raw source UI bundle is missing" >&2; exit 1; }
test -f ppl/dist/ppl.bundle.js || { echo "Build the PPL parser first: bash ppl/build.sh" >&2; exit 1; }
test -d ppl/dist/licenses || { echo "PPL third-party licenses are missing" >&2; exit 1; }
test -f .tabularium
test -f locales/en.json
test -f LICENSE
test -f NOTICE
mkdir -p bin/packages
stage=$(mktemp -d "$root/bin/package.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/ui/dist" "$stage/locales" "$stage/docs/tickets" "$stage/tests/host"
cp .tabularium README.md "$stage/"
cp docs/specification.md docs/implementation-plan.md docs/compatibility.md "$stage/docs/"
cp docs/tickets/001-opensearch-driver-v0.1.0.md "$stage/docs/tickets/"
cp tests/host/README.md "$stage/tests/host/"
cp ui/dist/index.js "$stage/ui/dist/"
cp ui/dist/raw-source.js "$stage/ui/dist/"
while IFS= read -r -d '' locale; do
  [[ "$locale" =~ ^locales/[A-Za-z0-9_-]+\.json$ ]] || { echo "Invalid tracked locale name" >&2; exit 1; }
  test -f "$locale" && test ! -L "$locale" || { echo "Invalid tracked locale file" >&2; exit 1; }
  cp "$locale" "$stage/locales/"
done < <(git ls-files -z -- 'locales/*.json')
cp ppl/dist/ppl.bundle.js "$stage/"
bash scripts/collect-third-party-notices.sh "$stage/licenses"
cp LICENSE NOTICE "$stage/"

build_package() {
  os=$1
  arch=$2
  label=$3
  binary=opensearch-plugin
  if [ "$os" = windows ]; then binary=opensearch-plugin.exe; fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -o "$stage/$binary" ./cmd/opensearch-plugin
  archive="$root/bin/packages/opensearch-plugin-$label.zip"
  rm -f "$archive"
  (cd "$stage" && zip -X -q -r "$archive" .)
  rm -f "$archive.sha256"
  rm "$stage/$binary"
}

build_package linux amd64 linux-x64
build_package linux arm64 linux-arm64
build_package darwin amd64 darwin-x64
build_package darwin arm64 darwin-arm64
build_package windows amd64 win-x64
build_package windows arm64 windows-aarch64

cp .tabularium "$root/bin/packages/default.tabularium"
rm -f "$root/bin/packages/default.tabularium.sha256"
assets=(default.tabularium)
for label in linux-x64 linux-arm64 darwin-x64 darwin-arm64 win-x64 windows-aarch64; do
  assets+=("opensearch-plugin-$label.zip")
done
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$root/bin/packages" && sha256sum "${assets[@]}" > SHA256SUMS)
else
  (cd "$root/bin/packages" && shasum -a 256 "${assets[@]}" > SHA256SUMS)
fi
