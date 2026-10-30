set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p .omx
scratch=$(mktemp -d "$root/.omx/tabularis-host-ci.XXXXXXXX")
network="tabularis-host-ci-$(basename "$scratch")"
search=""
host=""
cleanup() {
  [ -z "$host" ] || docker rm -fv "$host" >/dev/null 2>&1 || true
  [ -z "$search" ] || docker rm -fv "$search" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT

image=ghcr.io/tabularisdb/tabularis:0.30.0
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image" >/dev/null
arch=$(docker image inspect "$image" --format '{{.Architecture}}')
mkdir -p "$scratch/plugin/ui/dist" "$scratch/plugin/locales"
test -f ppl/dist/ppl.bundle.js || bash ppl/build.sh
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$scratch/plugin/opensearch-plugin" ./cmd/opensearch-plugin
cp .tabularium "$scratch/plugin/"
cp ppl/dist/ppl.bundle.js "$scratch/plugin/"
cp ui/dist/index.js ui/dist/raw-source.js "$scratch/plugin/ui/dist/"
cp locales/en.json "$scratch/plugin/locales/"

docker network create "$network" >/dev/null
search=$(docker run -d --network "$network" --network-alias search \
  -e discovery.type=single-node -e DISABLE_SECURITY_PLUGIN=true \
  -e node.store.allow_mmap=false -e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
  -p 127.0.0.1::9200 opensearchproject/opensearch:2.19.3)
endpoint="http://$(docker port "$search" 9200/tcp | head -1)"
ready=false
for attempt in $(seq 1 90); do
  if curl -fsS "$endpoint" >/dev/null 2>&1; then ready=true; break; fi
  sleep 2
done
if [ "$ready" != true ]; then docker logs "$search" >&2; exit 1; fi
curl -fsS -X POST "$endpoint/_bulk?refresh=true" -H 'Content-Type: application/x-ndjson' \
  --data-binary @tests/host/documents.ndjson | jq -e '.errors == false' >/dev/null

port=${TABULARIS_HOST_TEST_PORT:-18388}
origin="http://localhost:$port"
host=$(docker run -d --network "$network" -p "127.0.0.1:$port:8080" \
  -e "TABULARIS_PUBLIC_URL=$origin" \
  -e TABULARIS_WEB_PASSWORD=LocalTestPass2026 -e TABULARIS_SECRET_KEY=LocalTestSecretKey2026 \
  -v "$scratch/plugin:/data/.local/share/tabularis/plugins/drivers/opensearch:ro" "$image")
ready=false
for attempt in $(seq 1 60); do
  if curl -fsS "$origin/healthz" >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
if [ "$ready" != true ]; then docker logs "$host" >&2; exit 1; fi
curl -fsS -o /dev/null -c "$scratch/cookie" -H "Origin: $origin" \
  --data 'password=LocalTestPass2026' "$origin/api/v1/auth/login"
csrf=$(curl -fsS -b "$scratch/cookie" "$origin/api/v1/session" | jq -er '.csrfToken')
rpc() {
  curl -fsS -b "$scratch/cookie" -H "Origin: $origin" -H "x-tabularis-csrf: $csrf" \
    -H 'Content-Type: application/json' --data "$2" "$origin/api/v1/rpc/$1" | jq 'if .ok == true then .data else error(tostring) end'
}
connection() {
  payload=$(jq -nc --arg mode "$1" '{name:("Host test " + $mode),params:{driver:"opensearch",database:"default",extra:{endpoint:"http://search:9200",deployment:"self_hosted",auth_mode:"anonymous",index_pattern:"tabularis_host_fixture",verified_exact_fields:"count",pagination_mode:$mode}}}')
  rpc save_connection "$payload" | jq -er '.id'
}
query() {
  payload=$(jq -nc --arg id "$1" --arg sql "$2" --arg session "$3" --argjson page "$4" --argjson limit "$5" '{connectionId:$id,query:$sql,sessionId:$session,page:$page,limit:$limit}')
  rpc execute_query "$payload"
}
id=$(connection offset)
query "$id" 'SELECT * FROM "tabularis_host_fixture"' browse 1 10 | jq -e '.rows | length == 3' >/dev/null
query "$id" 'SELECT * FROM "tabularis_host_fixture" WHERE "_source.count" >= 4 ORDER BY "_source.count" DESC' filter 1 10 | jq -e '[.rows[][4]] == [5,4]' >/dev/null
ppl_query=$(printf '#!ppl\nsource=tabularis_host_fixture | fields count | sort count')
query "$id" "$ppl_query" ppl 1 2 | jq -e '[.rows[][0]] == [3,4] and .truncated == true' >/dev/null
pit=$(connection pit)
query "$pit" 'SELECT * FROM "tabularis_host_fixture"' pit-test 1 1 > "$scratch/page1.json"
query "$pit" 'SELECT * FROM "tabularis_host_fixture"' pit-test 2 1 > "$scratch/page2.json"
jq -e '.rows | length == 1' "$scratch/page1.json" "$scratch/page2.json" >/dev/null
if [ "$(jq -c '.rows[0]' "$scratch/page1.json")" = "$(jq -c '.rows[0]' "$scratch/page2.json")" ]; then echo 'PIT pages repeated a row' >&2; exit 1; fi
payload=$(jq -nc --arg id "$pit" '{connectionId:$id,sessionId:"pit-test"}')
curl -fsS "$endpoint/_search/point_in_time/_all" | jq -e '.pits | length == 1' >/dev/null
rpc release_query_session "$payload" >/dev/null
start=$SECONDS
released=false
for attempt in $(seq 1 100); do
  if curl -fsS "$endpoint/_search/point_in_time/_all" | jq -e '.pits | length == 0' >/dev/null; then released=true; break; fi
  sleep 1
done
if [ "$released" != true ]; then
  echo 'PIT remained open after the idle cleanup deadline' >&2
  docker logs "$host" >&2
  exit 1
fi
echo "PIT cleanup observed $((SECONDS - start)) seconds after host release request."
echo 'Released Tabularis host RPC integration passed.'
