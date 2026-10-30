set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
commit=520e766628a558bf9f3f7c8758846aac54b26242
cache="$root/.omx/ppl-upstream"
grammar="$cache/packages/osd-antlr-grammar/src/opensearch_ppl/.generated"

if [ ! -d "$cache/.git" ]; then
  mkdir -p "$cache"
  git -C "$cache" init -q
  git -C "$cache" remote add origin https://github.com/opensearch-project/OpenSearch-Dashboards.git
  git -C "$cache" fetch --quiet --depth=1 --filter=blob:none origin "$commit"
  git -C "$cache" sparse-checkout set packages/osd-antlr-grammar
  git -C "$cache" checkout --quiet --detach FETCH_HEAD
fi

test "$(git -C "$cache" rev-parse HEAD)" = "$commit"
printf '%s  %s\n' \
  71e048d2d06f9697fbca640d2b3ae9e7b994e1e0cd0178e324a6071a3590f2be "$grammar/OpenSearchPPLLexer.ts" \
  b842c353a6f01ac53a51c9b7ce996aec91c026761bc2a4493945948e78d5941f "$grammar/OpenSearchPPLParser.ts" \
  14241f4e0810a41e9f2cacfd040b10b9de836f01b1221fde27de78f8d4cdabe6 "$grammar/OpenSearchPPLParserVisitor.ts" \
  cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30 "$cache/LICENSE.txt" \
  5989381456476560bf1527f9cad85135e3ff8f8aba92f276d88708ff9f06b7a9 "$cache/NOTICE.txt" | shasum -a 256 -c

npm ci --prefix "$root/ppl" --no-audit --no-fund
mkdir -p "$root/ppl/dist/licenses"
NODE_PATH="$root/ppl/node_modules" "$root/ppl/node_modules/.bin/esbuild" "$root/ppl/adapter.ts" \
  --bundle --platform=neutral --format=iife --global-name=PPLParser \
  --target=es2020 --minify --legal-comments=none --outfile="$root/ppl/dist/ppl.bundle.js"

cd "$root/ppl/dist"
shasum -a 256 -c "$root/ppl/bundle.sha256"
cp "$cache/LICENSE.txt" licenses/opensearch-dashboards-LICENSE.txt
cp "$cache/NOTICE.txt" licenses/opensearch-dashboards-NOTICE.txt
cp "$root/ppl/node_modules/antlr4ng/LICENSE.txt" licenses/antlr4ng-LICENSE.txt
cp "$root/ppl/node_modules/esbuild/LICENSE.md" licenses/esbuild-LICENSE.md
cd "$root"
go mod download github.com/dop251/goja
goja_dir=$(go list -m -f '{{.Dir}}' github.com/dop251/goja)
install -m 0644 "$goja_dir/LICENSE" "$root/ppl/dist/licenses/goja-LICENSE.txt"
for module in github.com/dlclark/regexp2/v2 github.com/go-sourcemap/sourcemap github.com/google/pprof golang.org/x/text; do
  go mod download "$module"
  module_dir=$(go list -m -f '{{.Dir}}' "$module")
  name=$(printf '%s' "$module" | tr '/' '-')
  install -m 0644 "$module_dir/LICENSE" "$root/ppl/dist/licenses/$name-LICENSE.txt"
done
