# Tabularis OpenSearch Plugin

A Go driver for exploring OpenSearch from Tabularis. The project is under active development; the v0.1.0 specification is not yet fully implemented or validated. See the [specification](docs/specification.md), [implementation plan](docs/implementation-plan.md), and [ticket](docs/tickets/001-opensearch-driver-v0.1.0.md) for scope and acceptance criteria.

The plugin icon uses the official OpenSearch brand mark hosted on GitHub and requires network access to display.

## Features

- Connect to self-hosted OpenSearch, Amazon OpenSearch Service managed domains, and Amazon OpenSearch Serverless.
- Browse indices, inspect mappings, and view documents in the Tabularis data grid.
- Run read-only Query DSL, OpenSearch SQL, PPL, and allowlisted REST inspection requests.
- Authenticate with anonymous or basic access, the AWS default credential chain, a named AWS profile, or explicit AWS credentials, as appropriate for the deployment.
- Use bounded offset pagination or opt-in point-in-time pagination for supported document searches.

See [Limitations](#limitations) for query, pagination, and deployment restrictions.

## Installation

The plugin targets Tabularis 0.30.0 or later. No public release or registry installation is available yet. Build a snapshot package as described in [Snapshot packages](#snapshot-packages), then install the ZIP matching your operating system and architecture:

1. In Tabularis, open **Settings → Plugins → Open plugins folder**.
2. Extract the ZIP into `drivers/opensearch` under that folder. Keep `.tabularium`, the executable, `ppl.bundle.js`, `ui`, `locales`, and license files together.
3. In **Settings → Plugins**, select **Refresh**, then enable **OpenSearch**.
4. In **Connections**, select **Add Connection → OpenSearch**.

On macOS, the plugin directory is `~/Library/Application Support/tabularis/plugins/drivers/opensearch/`. The interpreter setting is optional and should remain empty for the compiled Go executable. If Tabularis is already using the plugin, restart it to load an updated executable or UI bundle.

## Connection configuration

Enter the full endpoint URL manually; it must not include credentials, a query string, or a fragment. Choose the deployment and authentication mode for your endpoint:

| Deployment | Authentication | Required connection details |
| --- | --- | --- |
| Self-hosted OpenSearch | Anonymous or basic | HTTP or HTTPS endpoint; username and password for basic authentication |
| Amazon OpenSearch Service managed domain | AWS default credential chain, named profile, explicit AWS keys, or basic | HTTPS endpoint and AWS region; credentials for the selected authentication mode |
| Amazon OpenSearch Serverless | AWS default credential chain, named profile, or explicit AWS keys | HTTPS collection endpoint and AWS region; credentials for the selected authentication mode |

For a named profile, select **AWS named profile** and enter its profile name. SSO-backed profiles need an active AWS login. Explicit AWS access keys and optional session tokens use Tabularis connection secrets. TLS verification is enabled by default; a custom CA can be configured through Tabularis. Do not use basic credentials over unencrypted HTTP.

The connection test lists indices. A successful test does not guarantee permission for every operation, and a denied test may mean that the selected credentials or policies do not allow index listing. Check the selected authentication mode, region, signing, and applicable access and network policies.

## Current query behavior

Run queries in the built-in Tabularis console. The console uses SQL editor highlighting; select the OpenSearch query format with the prefix on the first line.

| Prefix | Query type |
| --- | --- |
| `#!dsl` | JSON search envelope with an index and search body |
| `#!rest` | Allowlisted read-only REST inspection |
| `#!sql` | Validated OpenSearch SQL subset |
| `#!ppl` | Validated Piped Processing Language subset |
| None | Simple table queries handled by the document browser |

Grid filters and sorts work for explicitly verified top-level fields on a concrete index. In the connection form, list fields only after verifying that they are single-valued, have no `null_value` or normalizer, and have matching SQL/DSL semantics across the connection. Every non-null value must be visible to `exists`, including values affected by `ignore_above`, `ignore_malformed`, and indexing settings. Verified browsing requires permission to resolve the index and read its settings, and checks its UUID before mapping and after search. Keep index identity and mappings stable during queries: these checks cannot detect every concurrent change. Unverified, ambiguous and unsupported fields are rejected.

For example, execute a search in the Tabularis editor:

```text
#!dsl
{"index":"events","body":{"query":{"match_all":{}},"size":100}}
```

For an aggregation without document hits, set `size` to `0`. The response is returned as structured JSON:

```text
#!dsl
{"index":"events","body":{"size":0,"aggs":{"total":{"sum":{"field":"count"}}}}}
```

Replace `events` and `count` with an index and numeric field from your mapping.

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

Numbered offset pages do not provide snapshot consistency. Native SQL pages after the first require `ORDER BY`. The connection form also offers an opt-in PIT mode for document searches: it requires a saved connection, a host query session, one concrete one-primary-shard index, and permission to resolve the index, read its settings, and create, search, and close a PIT. It sorts on `_id`, which may consume significant server memory. The built-in JSON viewer may round large numbers; use the **View raw document JSON** row action for the original source text.

## Limitations

- Connection testing requires permission to list indices, even when the profile can query a known index. A successful test does not establish permission for every query.
- SQL and PPL support validated read-only subsets. SQL cursor responses and PPL pagination are not supported; scripted DSL searches are rejected.
- Grid filtering and sorting require explicitly verified top-level fields. Nested, dotted, ambiguous, and unsupported field mappings are rejected.
- The explorer lists index names; aliases, settings, and statistics do not have dedicated explorer views. Supported inspection endpoints are available through `#!rest`.
- In Tabularis 0.30.0, PIT cleanup is verified through idle expiry rather than immediate tab-close release. See [host-test notes](tests/host/README.md).
- The client does not automatically retry requests. Cross-compiled packages are not evidence of native installation on every platform.

## TODO

- Add an OpenSearch-specific console as a top-level editor tab, selectable alongside the SQL console from the **+** button, when Tabularis officially supports plugin-provided editor tabs and tab creation.
- Expand safe pagination, field filtering, and metadata browsing, and add bounded retries.
- Complete AWS compatibility testing, native platform validation, the compatibility matrix, and third-party notices before release.

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
cd ..
```

The Go executable communicates using newline-delimited JSON-RPC on standard input and output. Diagnostics go to standard error. The UI bundle is produced at `ui/dist/index.js`; `.tabularium` is the driver manifest.

### Local integration tests

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

Do not run test setup writes against formal data sources. Read-only metadata checks have passed against a managed domain and a Serverless vector collection. The full AWS compatibility matrix, including SSO credential refresh, multiple accounts and regions, and Serverless search and time-series collections, remains unverified.

`bash scripts/ci-tabularis-host.sh` exercises the driver through the released Tabularis Web HTTP RPC interface using isolated disposable containers. It does not test the browser UI. See [host-test notes](tests/host/README.md) for the released host's sign-in and PIT cleanup behavior.

### Snapshot packages

After building the UI, `scripts/package.sh` builds snapshot ZIPs and SHA-256 checksums under `bin/packages/` for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. These artifacts are not releases and are not automatically published. Cross-compilation does not establish native runtime compatibility.

```sh
bash scripts/package.sh
bash scripts/smoke-packages.sh
```

Release publication requires the specification's compatibility, third-party notice, version and platform-verification gates to be satisfied.

## Acknowledgments

The [Tabularis Elasticsearch plugin](https://github.com/TabularisDB/tabularis-elasticsearch-plugin) informed this project's Tabularis integration and query-mode design. Thanks to its maintainers and contributors.

## License

Apache License 2.0. See [LICENSE](LICENSE).
