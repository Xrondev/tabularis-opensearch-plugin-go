set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
output=${1:?Output directory is required}
test ! -e "$output" || { echo "Notice output already exists: $output" >&2; exit 1; }
test -d ppl/dist/licenses || { echo "Build the PPL parser and its licenses first" >&2; exit 1; }
mkdir -p "$output"

# 各目标平台的 Go package 清单必须一致。
CGO_ENABLED=0 go list -deps -f '{{if .Module}}{{.ImportPath}}{{end}}' ./cmd/opensearch-plugin | LC_ALL=C sort -u > "$output/runtime-packages.txt"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go list -deps -f '{{if .Module}}{{.ImportPath}}{{end}}' ./cmd/opensearch-plugin | LC_ALL=C sort -u > "$output/target-packages.txt"
  if ! cmp -s "$output/runtime-packages.txt" "$output/target-packages.txt"; then
    echo "Go dependency packages differ for $target; audit its licenses separately" >&2
    exit 1
  fi
done
rm "$output/target-packages.txt"

# go-licenses 无法识别 Lucent license，因此单独收集原文。
CGO_ENABLED=0 go run github.com/google/go-licenses/v2@v2.0.1 save ./cmd/opensearch-plugin \
  --save_path="$output/go" \
  --ignore tabularis-opensearch-plugin-go \
  --ignore github.com/dop251/goja/ftoa
test -f "$output/go/github.com/dop251/goja/LICENSE"
test -f "$output/go/github.com/aws/aws-sdk-go-v2/LICENSE.txt"
test -f "$output/go/github.com/opensearch-project/opensearch-go/v5/NOTICE.txt"
goja_dir=$(go list -m -f '{{.Dir}}' github.com/dop251/goja)
mkdir -p "$output/go/github.com/dop251/goja/ftoa/internal/fast"
cp "$goja_dir/ftoa/LICENSE_LUCENE" "$output/go/github.com/dop251/goja/ftoa/"
cp "$goja_dir/ftoa/internal/fast/LICENSE_V8" "$output/go/github.com/dop251/goja/ftoa/internal/fast/"
for module in golang.org/x/sync golang.org/x/text; do
  module_dir=$(go list -m -f '{{.Dir}}' "$module")
  cp "$module_dir/PATENTS" "$output/go/$module/PATENTS"
done

goroot=$(go env GOROOT)
mkdir -p "$output/go-stdlib"
cp "$goroot/LICENSE" "$goroot/PATENTS" "$output/go-stdlib/"
mkdir -p "$output/go-stdlib/crypto/internal/boring"
cp "$goroot/src/crypto/internal/boring/LICENSE" "$output/go-stdlib/crypto/internal/boring/"
for module in crypto net sys text; do
  target="$output/go-stdlib/vendor/golang.org/x/$module"
  mkdir -p "$target"
  cp "$goroot/src/vendor/golang.org/x/$module/LICENSE" "$goroot/src/vendor/golang.org/x/$module/PATENTS" "$target/"
done
cp -R ppl/dist/licenses "$output/ppl"
