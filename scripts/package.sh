set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
test -f ui/dist/index.js || { echo "Build the UI first: cd ui && npm ci && npm run build" >&2; exit 1; }
test -f ui/dist/raw-source.js || { echo "Raw source UI bundle is missing" >&2; exit 1; }
test -f .tabularium
test -f locales/en.json
mkdir -p bin/packages
stage=$(mktemp -d "$root/bin/package.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/ui/dist" "$stage/locales"
cp .tabularium README.md "$stage/"
cp ui/dist/index.js "$stage/ui/dist/"
cp ui/dist/raw-source.js "$stage/ui/dist/"
cp locales/*.json "$stage/locales/"
if [ -f LICENSE ]; then cp LICENSE "$stage/"; fi
if [ -f NOTICE ]; then cp NOTICE "$stage/"; fi

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
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$root/bin/packages" && sha256sum "$(basename "$archive")" > "$(basename "$archive").sha256")
  else
    (cd "$root/bin/packages" && shasum -a 256 "$(basename "$archive")" > "$(basename "$archive").sha256")
  fi
  rm "$stage/$binary"
}

build_package linux amd64 linux-x64
build_package linux arm64 linux-arm64
build_package darwin amd64 darwin-x64
build_package darwin arm64 darwin-arm64
build_package windows amd64 win-x64

if [ ! -f LICENSE ]; then
  echo "Snapshot only: LICENSE is not present; publication requires a confirmed license." >&2
fi
