# Tabularis OpenSearch Plugin

A Go driver for exploring OpenSearch from Tabularis. The project is under active development; the v0.1.0 specification is not yet fully implemented or validated. See the [specification](docs/specification.md), [implementation plan](docs/implementation-plan.md), and [ticket](docs/tickets/001-opensearch-driver-v0.1.0.md) for scope and acceptance criteria.

## Development

Requirements: the Go version in `go.mod`, Node.js 20.19 or newer, npm, and Docker for real OpenSearch integration tests.

Build the pinned PPL parser artifact before packaging or installing the driver:

```sh
bash ppl/build.sh
```

The packaged Go executable loads this integrity-checked artifact next to the executable; Node.js is not required at runtime.

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

`bash scripts/ci-tabularis-host.sh` exercises the driver through the released Tabularis Web HTTP RPC interface using isolated disposable containers. It does not test the browser UI. See [host-test notes](tests/host/README.md) for the released host's sign-in and PIT cleanup behavior.

## Snapshot packages

After building the UI, `scripts/package.sh` builds snapshot ZIPs and SHA-256 checksums under `bin/packages/` for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. These artifacts are not releases and are not automatically published. Cross-compilation does not establish native runtime compatibility.

```sh
bash scripts/package.sh
bash scripts/smoke-packages.sh
```

Release publication requires the specification's compatibility, third-party notice, version and platform-verification gates to be satisfied.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Current query behavior

Use `#!dsl` for a JSON search envelope, `#!rest` for allowlisted inspection, `#!sql` for the validated native SQL subset, or `#!ppl` for the validated PPL subset. Unprefixed simple table queries use the browse adapter. Grid filters and sorts work for explicitly verified top-level fields on a concrete index. In the connection form, list fields only after verifying that they are single-valued, have no `null_value` or normalizer, and have matching SQL/DSL semantics across the connection. Every non-null value must be visible to `exists`, including values affected by `ignore_above`, `ignore_malformed`, and indexing settings. Verified browsing requires permission to resolve the index and read its settings, and checks its UUID before mapping and after search. Keep index identity and mappings stable during queries: these checks cannot detect every concurrent change. Unverified, ambiguous and unsupported fields are rejected.

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

```text
#!ppl
source=events | fields count | sort count
```

In the connection form, enter the endpoint, deployment, authentication mode and, for AWS, the region and credential mode. Explicit AWS credential values use Tabularis connection secrets. Self-hosted HTTP endpoints require explicit configuration; HTTPS verifies certificates and supports a custom CA.

Numbered offset pages do not provide snapshot consistency. Native SQL pages after the first require `ORDER BY`. The connection form also offers an opt-in PIT mode for document searches: it requires a saved connection, a host query session, one concrete one-primary-shard index, and permission to resolve the index, read its settings, and create, search, and close a PIT. It sorts on `_id`, which may consume significant server memory. The built-in JSON viewer may round large numbers; use the **View raw document JSON** row action for the original source text.
