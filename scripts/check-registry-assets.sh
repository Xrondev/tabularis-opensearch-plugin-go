set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
assets=${1:?Asset directory is required}
mkdir -p "$root/.omx"
scratch=$(mktemp -d "$root/.omx/registry-assets.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT

revision=127db6dc79db139f203cc3eb8a97bdf4d73faf68
source="$scratch/webhook.ts"
curl -fsSL --retry 3 "https://raw.githubusercontent.com/TabularisDB/tabularium/$revision/apps/api/src/lib/webhook.ts" -o "$source"
printf '%s  %s\n' 2bf860b8733a198ac6552a702067f9630c5aeffe4bacd6ff844c646aa091c822 "$source" | shasum -a 256 -c
"$root/ui/node_modules/.bin/esbuild" "$source" --bundle --platform=node --format=esm --outfile="$scratch/webhook.mjs" >/dev/null
node "$root/tests/registry-asset-layout.mjs" "$scratch/webhook.mjs" "$assets"
