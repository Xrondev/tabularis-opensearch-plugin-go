# Tabularis OpenSearch Plugin

A Go driver for exploring OpenSearch from Tabularis. The project is under active development; the v0.1.0 specification is not yet fully implemented or validated. See the [specification](docs/specification.md), [implementation plan](docs/implementation-plan.md), and [ticket](docs/tickets/001-opensearch-driver-v0.1.0.md) for scope and acceptance criteria.

## Development

Requirements: the Go version in `go.mod`, Node.js 20.19 or newer, npm, and Docker for real OpenSearch integration tests.

```sh
go test -race ./...
go vet ./...
```

Build and test the connection UI:

```sh
cd ui
npm ci
npm run typecheck
npm test
npm run build
```

The Go executable communicates using newline-delimited JSON-RPC on standard input and output. Diagnostics go to standard error. The UI bundle is produced at `ui/dist/index.js`; `.tabularium` is the driver manifest.

## Local integration tests

`scripts/ci-opensearch.sh` creates a disposable OpenSearch Docker container and fixture indices, runs the real integration tests, and removes that specific container when finished. It binds an automatically selected port to loopback and does not use an existing data source. The script creates and writes fixture documents only inside its own disposable container.

```sh
bash scripts/ci-opensearch.sh 2.19.3
bash scripts/ci-opensearch.sh 3.3.2
```

To run tests against an existing authorized test instance, use read-only access and provide the fixture names. The fixture used by pagination tests must contain documents with `count` values 3, 4, and 5; the SQL fixture must match the integration test's documented assertions.

```sh
OPENSEARCH_TEST_ENDPOINT=http://127.0.0.1:9200 \
OPENSEARCH_TEST_INDEX=tabularis_fixture \
OPENSEARCH_TEST_FIXTURE_INDEX=tabularis_pit_fixture \
go test -race ./...
```

Do not run test setup writes against formal data sources. AWS managed-domain and Serverless integration require authorized endpoints, policies and credentials; their validation is pending.

## Snapshot packages

After building the UI, `scripts/package.sh` builds snapshot ZIPs and SHA-256 checksums under `bin/packages/` for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. These artifacts are not releases and are not automatically published. Cross-compilation does not establish native runtime compatibility.

```sh
bash scripts/package.sh
bash scripts/smoke-packages.sh
```

Release publication requires the specification's compatibility, license, notice, version and platform-verification gates to be satisfied.

## Current query behavior

Use `#!dsl` for a JSON search envelope, `#!rest` for allowlisted inspection, or `#!sql` for the validated native SQL subset. Unprefixed simple table queries use the browse adapter. The grid's generated field filters and sorts require verified field semantics; the current connection path rejects them when that evidence is unavailable. Unsupported syntax is rejected. PPL is not implemented.

For example, execute a search in the Tabularis editor:

```text
#!dsl
{"index":"events","body":{"query":{"match_all":{}},"size":100}}
```

Inspect an index mapping or execute native SQL:

```text
#!rest
GET /events/_mapping
```

```text
#!sql
SELECT COUNT(*) FROM "events"
```

In the connection form, enter the endpoint, deployment, authentication mode and, for AWS, the region and credential mode. Explicit AWS credential values use Tabularis connection secrets. Self-hosted HTTP endpoints require explicit configuration; HTTPS verifies certificates and supports a custom CA.

Numbered offset pages do not provide snapshot consistency. Native SQL pages after the first require `ORDER BY`. The connection form also offers an opt-in PIT mode for document searches: it requires a saved connection, a host query session, one concrete one-primary-shard index, and permission to resolve the index, read its settings, and create, search, and close a PIT. It sorts on `_id`, which may consume significant server memory. The built-in JSON viewer may round large numbers; use the **View raw document JSON** row action for the original source text.
