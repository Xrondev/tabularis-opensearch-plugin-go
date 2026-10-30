set -euo pipefail

version=${1:?OpenSearch image version is required}
case "$version" in
  2.19.3|3.3.2) ;;
  *) echo "Unsupported test image: $version" >&2; exit 1 ;;
esac

container=$(docker run -d --rm \
  -e 'discovery.type=single-node' \
  -e 'DISABLE_SECURITY_PLUGIN=true' \
  -e 'node.store.allow_mmap=false' \
  -e 'OPENSEARCH_JAVA_OPTS=-Xms512m -Xmx512m' \
  -p 127.0.0.1::9200 \
  "opensearchproject/opensearch:$version")
trap 'docker rm -f "$container" >/dev/null' EXIT
address=$(docker port "$container" 9200/tcp | head -1)
endpoint="http://$address"

ready=false
for attempt in $(seq 1 90); do
  if curl -fsS "$endpoint" >/dev/null 2>&1; then ready=true; break; fi
  sleep 2
done
if [ "$ready" != true ]; then
  docker logs "$container" >&2
  exit 1
fi

curl -fsS -X PUT "$endpoint/tabularis_fixture/_doc/1?refresh=true" \
  -H 'Content-Type: application/json' -d '{"message":"hello","count":3}' >/dev/null
curl -fsS -X PUT "$endpoint/tabularis_fixture/_doc/2?refresh=true" \
  -H 'Content-Type: application/json' -d '{"message":"two","count":4}' >/dev/null
curl -fsS -X PUT "$endpoint/tabularis_fixture/_doc/3?refresh=true" \
  -H 'Content-Type: application/json' -d '{"message":"three","count":5}' >/dev/null
for count in 3 4 5; do
  curl -fsS -X PUT "$endpoint/tabularis_pit_fixture/_doc/$count?refresh=true" \
    -H 'Content-Type: application/json' -d "{\"count\":$count,\"message\":\"hello\"}" >/dev/null
done

curl -fsS -X PUT "$endpoint/tabularis_deep_fixture" \
  -H 'Content-Type: application/json' \
  -d '{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":{"properties":{"sequence":{"type":"long"}}}}' >/dev/null
{
  for number in $(seq 1 10005); do
    printf '{"index":{"_id":"%s"}}\n{"sequence":%s}\n' "$number" "$number"
  done
} | curl -fsS -X POST "$endpoint/tabularis_deep_fixture/_bulk?refresh=true" \
  -H 'Content-Type: application/x-ndjson' --data-binary @- | jq -e '.errors == false' >/dev/null

OPENSEARCH_TEST_ENDPOINT="$endpoint" \
OPENSEARCH_TEST_INDEX=tabularis_fixture \
OPENSEARCH_TEST_FIXTURE_INDEX=tabularis_pit_fixture \
OPENSEARCH_DEEP_TEST_INDEX=tabularis_deep_fixture \
go test -race ./... -count=1 -timeout=5m
