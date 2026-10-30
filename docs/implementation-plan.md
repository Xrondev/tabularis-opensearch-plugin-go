# Tabularis OpenSearch Plugin — v0.1.0 implementation plan

Status: researched plan; implementation has not started.

Detailed deliverables: [specification](specification.md) and [implementation ticket OS-001](tickets/001-opensearch-driver-v0.1.0.md).

Research date: 2026-10-30. Repository baseline: empty repository, no commits or application files. This document defines the proposed implementation, acceptance gates, and release evidence. It does not claim database compatibility has been tested.

## 1. Requirements and boundaries

Implement a Go driver for self-hosted OpenSearch, Amazon OpenSearch Service managed domains, and Amazon OpenSearch Serverless collection endpoints. Preserve all requested feature areas: connection management, index/alias metadata, document inspection, SQL, PPL, Query DSL, allowlisted REST inspection, bounded and deep pagination, cancellation, secure credentials, and cross-platform distribution.

Target OpenSearch 2.x and 3.x through an explicitly tested version matrix. Feature availability is operation-, version-, permission-, and collection-dependent. Serverless search, time-series, and vector-search collections each require their own compatibility evidence.

All database operations are read-oriented. Document/index mutations, administration, ingestion, provisioning, dashboards, and a custom query editor are outside v0.1.0. PIT creation/closure and query cursor closure are narrowly scoped transient-resource operations, not permission to mutate documents or configuration.

Proposed identity: driver `opensearch`, executable `opensearch-plugin`, repository `tabularis-opensearch-plugin-go`, initial version `0.1.0`. The project is licensed under Apache-2.0. No deployment or formal data-source writes are authorized by this plan.

## 2. Evidence baseline

Source snapshots inspected locally under ignored `.omx/research/`:

- Tabularis host: `92820f0df1fc5ff826ec03bec5a8119ae1d73c14`.
- Elasticsearch plugin: `d3b647ac5414359c6146dda96ce74efe8506943d`.
- DynamoDB plugin: `ad72a09b84edc56cc5b6ddaebba06d490a444978`.
- Redis Go plugin: `3ccf5ee342cd6a891ec4e483c5c3ad1868a289d0`.

Useful implementation references:

- Elasticsearch demonstrates index-to-table mapping, mapping inspection, and explicit editor mode prefixes. See [dispatch](https://github.com/TabularisDB/tabularis-elasticsearch-plugin/blob/d3b647ac5414359c6146dda96ce74efe8506943d/src/rpc.rs#L29-L61), [metadata](https://github.com/TabularisDB/tabularis-elasticsearch-plugin/blob/d3b647ac5414359c6146dda96ce74efe8506943d/src/handlers/metadata.rs#L46-L110), and [query-mode dispatch](https://github.com/TabularisDB/tabularis-elasticsearch-plugin/blob/d3b647ac5414359c6146dda96ce74efe8506943d/src/handlers/models.rs#L19-L45).
- DynamoDB demonstrates AWS connection normalization, provider configuration, and a connection-fields UI. See [connection normalization](https://github.com/TabularisDB/tabularis-dynamodb-plugin/blob/ad72a09b84edc56cc5b6ddaebba06d490a444978/src/handlers/connection.rs#L50-L148), [client configuration](https://github.com/TabularisDB/tabularis-dynamodb-plugin/blob/ad72a09b84edc56cc5b6ddaebba06d490a444978/src/dynamodb/pool.rs#L25-L114), and [UI fields](https://github.com/TabularisDB/tabularis-dynamodb-plugin/blob/ad72a09b84edc56cc5b6ddaebba06d490a444978/ui/src/extraFields.ts#L13-L44).
- Redis Go demonstrates Go request types, raw JSON request IDs, executable organization, and cross-platform packaging. See [protocol types](https://github.com/gzamboni/tabularis-redis-plugin-go/blob/3ccf5ee342cd6a891ec4e483c5c3ad1868a289d0/internal/plugin/types.go#L15-L31), [dispatcher](https://github.com/gzamboni/tabularis-redis-plugin-go/blob/3ccf5ee342cd6a891ec4e483c5c3ad1868a289d0/internal/plugin/plugin.go#L89-L167), and [release configuration](https://github.com/gzamboni/tabularis-redis-plugin-go/blob/3ccf5ee342cd6a891ec4e483c5c3ad1868a289d0/.goreleaser.yaml#L1-L24).

The new driver's wire types must be verified against the selected released Tabularis host. Primary code references are [ConnectionParams](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L186-L313), [metadata and QueryResult](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L469-L559), and [driver dispatch](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugins/driver.rs#L1235-L1403). The website's [plugin overview](https://tabularis.dev/wiki/plugins), [protocol reference](https://tabularis.dev/wiki/plugin-protocol), and [UI reference](https://tabularis.dev/wiki/ui-extensions) supply the public integration guidance.

## 3. Architecture and integration decisions

### Runtime and modules

Use the official `opensearch-go` client and AWS SDK for Go v2. Keep API adapters separated by operation, with deployment capabilities as data rather than three complete driver implementations. The current official client release inspected is [v5.1.3](https://github.com/opensearch-project/opensearch-go/releases/tag/v5.1.3); its [go.mod](https://github.com/opensearch-project/opensearch-go/blob/v5.1.3/go.mod) requires Go 1.26.0 and declares toolchain 1.26.9. Local Go is 1.25.4: toolchain setup is an implementation prerequisite. Pin dependencies and the CI toolchain after the compatibility spike.

Planned files/modules, not existing implementation:

- `cmd/opensearch-plugin/main.go`: startup, stdio wiring, process shutdown.
- `internal/protocol/`: exact host DTOs, newline framing, dispatch, request cancellation.
- `internal/connection/`: validated configuration, AWS provider selection, isolated client lifetime.
- `internal/opensearch/`: transport, signing, API operations, backend error classification.
- `internal/capability/`: deployment/version facts, operation support and authorization state.
- `internal/metadata/`: index/alias discovery, mapping traversal, field capabilities.
- `internal/query/`: mode selection, SQL/PPL/DSL execution, document lookup.
- `internal/safety/`: parsed-query policy and method/path/body validation.
- `internal/pagination/`: PIT/cursor ownership, page checkpoints, expiry and cleanup.
- `internal/result/`: native responses to host metadata and row matrices.
- `ui/`: TypeScript/React connection fields and narrowly scoped inspection extensions.
- `tests/integration/`, `testdata/`, `deploy/test/`, `.github/workflows/`, `docs/`.

### Tabularis protocol

Use one newline-delimited JSON-RPC 2.0 request/response per line. Keep stdout protocol-only. Read stdin while requests execute, bound concurrency and message/body sizes, serialize writes, preserve request IDs, and fail on output errors. Keep secrets and query bodies out of diagnostics. The host reuses a plugin process across connections. [Host lifecycle and cancellation](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugins/driver.rs#L300-L340)

Implement:

- `initialize`: non-secret process defaults only.
- `test_connection`, `ping`: genuine permitted read requests; no success based only on parsing configuration.
- `get_databases`: one implicit logical database; `get_schemas`: empty, with schemas disabled.
- `get_tables`: OpenSearch index names; aliases inspectable as alias metadata, with target indices and filters preserved.
- `get_columns`: native field metadata including recursive paths, multi-fields, nested/object boundaries, and specialized types.
- `get_foreign_keys`, `get_indexes`, `get_views`, `get_routines`: empty where the relational concept is inapplicable. OpenSearch indices belong in `get_tables`, not relational secondary-index metadata.
- `execute_query`: outer arguments `params`, `query`, `limit`, `page`, `schema`, and optional `session_id`; connection values are inside `params.params` on the wire.
- `release_session`: close owned pagination resources using the session ownership registry. The host sends only `session_id` for this method.
- `cancel`: notification with original request ID at `params.id`; cancel the Go context, emit no notification response, tolerate repeated/late cancellation.
- Optional `get_connection_metadata` only after released-host and registry-schema validation; return documented capabilities/types, not invented query-language flags.

Return `QueryResult` with `columns`, rectangular `rows`, `affected_rows: 0`, `truncated`, and optional `pagination: {page,page_size,total_rows,has_more}`. Preserve unknown totals as null. Additional results may carry structured aggregation output if verified in the chosen host. Unknown methods use JSON-RPC `-32601` with `Method not found`; validate parameters with `-32602`. Reject mutation RPCs explicitly, regardless of UI visibility. [Host result types](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L532-L559)

Manifest: canonical `.tabularium`, read-only, single implicit database, no table management, schemas/views/routines disabled, appropriate identifier quoting, and only implemented capabilities enabled. Set `min_runtime_version` to a released host tested with the required UI secret API and session behavior. The UI documentation identifies v0.29.0 for plugin-owned secret fields; a checked-out main commit is not a release guarantee. [Manifest guidance](https://tabularis.dev/wiki/plugins)

### Connection and credential model

Represent endpoint, deployment (`self_hosted`, `aws_managed`, `aws_serverless`), region, authentication mode, profile, timeout, page size, and CA configuration explicitly. These are proposed plugin-owned settings, not claimed host-native API fields. Freeze their exact key names and validation in milestone 1.

- Self-hosted: HTTP/HTTPS, anonymous and Basic authentication, system trust plus optional custom CA. Require explicit HTTP configuration; never downgrade HTTPS on failure.
- AWS managed: SigV4 service `es`; permit an explicitly selected Basic-auth mode for domains configured for it.
- AWS Serverless: HTTPS and SigV4 service `aoss`; use the collection endpoint directly and require region. Do not require AWS control-plane listing permissions.
- AWS provider modes: default chain, named profile/SSO profile, or explicit access key/secret/session token. SDK providers retain refresh behavior; explicit credentials are replaced by the user when expired. No automatic browser login or credential-provider fallback after an explicitly selected mode fails.
- Use host password storage for Basic authentication and `setSecretField` for AWS credential material. Persist only non-secret settings through `setExtraField`; the host resolves secrets into `params.extra` immediately before calls. Never put credentials in global plugin settings or endpoint URLs. [Host secret resolution](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugin_secrets.rs#L1-L9)
- Cache clients/providers by connection ID plus a private configuration fingerprint. Include deployment, region, endpoint, auth mode, profile, TLS settings and credential changes. Ad-hoc unsaved connections need isolated identities. Never expose fingerprints or raw credentials in logs. Bound cache size/idle time; configuration changes invalidate associated sessions.
- Disable redirects or enforce a same-origin policy before any signed/authenticated request. Validate endpoint URLs and encode index/document path segments. Bound retries to explicitly retryable reads, with deadlines and jitter; do not retry authentication/authorization failures indiscriminately.
- Configure fixed-endpoint routing for AWS and disable startup and periodic node discovery. Verify the selected client version's defaults explicitly; [v5.0.3 release notes](https://github.com/opensearch-project/opensearch-go/releases/tag/v5.0.3) describe periodic discovery behavior. Verify Serverless payload hashing, including `x-amz-content-sha256`, with signed real requests. [Official Go client](https://docs.opensearch.org/latest/clients/go/), [AWS Serverless clients](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-clients.html), [AWS credential configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html)

### Query modes, documents, and native table browsing

Proposed editor syntax is explicit `#!sql`, `#!ppl`, `#!dsl`, and `#!rest`; unprefixed input is SQL for host-generated table queries. Prefix parsing is only a small plugin envelope; use maintained parsers or upstream-generated grammars for actual SQL/PPL, and Go `encoding/json` for DSL bodies. Validate the entire query before sending it.

SQL and PPL use the relevant OpenSearch plugin APIs. DSL mode uses a documented plugin envelope containing index selection and the native search body; exact syntax must be frozen with editor tests in milestone 1. REST mode uses a method, relative path, and optional JSON body. SQL/PPL tunneled through REST receive the same safety validation as dedicated modes.

The host opens table tabs with SQL and reconstructs filtering/sorting as SQL. Therefore document browsing must include a parser-backed adapter for the exact host-generated SELECT/filter/sort/count forms, translated into native search/count requests where supported. This gives document metadata and PIT-backed browsing without relying on SQL being enabled for every endpoint. Routing is syntax-based because the wire arguments do not identify table-generated SQL: explicit `#!sql` always selects native SQL; an unprefixed query in the proven browse subset selects the adapter regardless of where it was entered; remaining validated read SQL selects the native SQL API when available. Unsupported syntax or unavailable native SQL fails clearly. Define null/missing, text/keyword, nested-field and ordering semantics in the query guide and test identical query text from table and editor paths. This integration must be demonstrated before building the explorer. [Table opening](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/packages/web-ui/src/utils/databaseObjectActions.ts#L181-L218), [query reconstruction](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/packages/web-ui/src/utils/editor.ts#L406-L441)

Document results preserve `_index`, `_id`, `_score`, and full `_source`, including null/absent distinctions where representable. Build scalar columns from mappings plus the bounded page's documents, not one document. Namespace source-field collisions with metadata columns. Keep objects/arrays as JSON, preserve large numeric values without float64 rounding, and document host JavaScript precision limits. Document lookup uses a safely encoded ID and supports routing where required. Aggregations and search diagnostics remain inspectable JSON; partial-shard failures and timeouts must be visible, not presented as complete results.

### Read-only policy

Apply defense in depth: read-only manifest, rejected mutation RPCs, parser-backed SQL/PPL policy, method/path/body allowlists, and least-privilege backend credentials. Validate every execution path, including REST and batch paths. Do not classify safety with a first-word check or regular expressions.

SQL policy admits supported read statement ASTs and rejects mutating or unknown statement forms. PPL policy admits the documented read/aggregation pipeline while rejecting training, persistence, and other side-effecting commands. The parser/library feasibility spike is a release blocker: no hand-written SQL/PPL parser and no unvalidated pass-through. OpenSearch documents SQL DELETE and PPL ML training, so safety must be established at statement/command level. [SQL DELETE](https://docs.opensearch.org/latest/sql-and-ppl/sql/delete/), [PPL ML](https://observability.opensearch.org/docs/ppl/commands/ml/)

Define a finite operation registry covering search, document get, count, mappings, settings, field capabilities, aliases, and selected explain APIs. Each entry specifies supported methods, path structure, query parameters, body validation, deployment support and resource effects. Reject absolute URLs, traversal/encoded-path bypasses, bulk/multi-operation envelopes outside policy, and unregistered endpoints. Internal PIT/cursor cleanup is limited to IDs created and owned by this process/session, never wildcard deletion.

### Capabilities and errors

Track supported, unsupported, unauthorized, and unknown independently. A forbidden response is not proof of an unsupported API. Use a deployment baseline plus lazy bounded reads; do not require cluster administration APIs for connection testing. Provide index-pattern configuration when permissions allow index reads but prohibit global listing.

Separate transport/TLS, credentials, permission, invalid query, unsupported API, expired pagination, and partial results in errors. Preserve sanitized backend status/type/request ID where available without inventing backend status codes. Optional unavailable metadata is shown as unavailable; counts/storage sizes are never synthesized as zero.

### AWS and version-specific capability baseline

The current [Serverless operation matrix](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-genref.html) lists SQL, PPL, PIT, aliases and document/metadata reads. Treat them as test candidates on each collection type, not universal guarantees. Serverless index listings omit some cluster-status fields; those fields must remain optional. Record collection generation as well as collection type in the matrix. [Serverless overview](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-overview.html)

AWS documentation and test fixtures must distinguish network policy, IAM data-plane access, and Serverless data-access policy. Relevant read permissions include `aoss:ReadDocument`, `aoss:DescribeIndex`, and alias-related `aoss:DescribeCollectionItems`; IAM API access uses `aoss:APIAccessAll`. Determine exact resource scopes per operation and do not require Dashboards access for this API client. Managed SQL resource names can occur inside request bodies, so URL scoping alone is not a read-only boundary. [Serverless IAM](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/security-iam-serverless.html), [data access](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-data-access.html), [managed SQL security](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/sql-support.html)

## 4. Pagination and lifecycle

The inspected host exchanges numbered pages and optional session IDs. It does not forward a general query cursor in the inspected `execute_query` path. Maintain remote cursors in the plugin, not in invented host result fields. [Host calls](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugins/driver.rs#L1235-L1327)

- Bound ordinary `from`/`size` requests by the endpoint's result window and configured limits.
- For deep browsing, use PIT plus `search_after` with a verified deterministic sort/tie-breaker. Detect availability; PIT requires compatible versions and permissions.
- Key pagination state by connection generation, session, query/mode/index fingerprint, sort, and page size. Retain bounded checkpoints for previous pages. Reject an unavailable random page with an actionable message rather than unbounded replay or a full-index download.
- With no stable session ID, avoid shared mutable query cursors; use bounded stateless pagination and explicit deep-pagination limitations. Verify all relevant table/editor paths in the chosen release before claiming deep browsing works there.
- Native SQL/PPL cursors are used only for query forms/backends with verified continuation and closure behavior. Aggregations/joins need independent bounded-result tests.
- OpenSearch PIT starts at 2.4; verify behavior below that boundary separately. SQL continuation pages may omit schema, so retain session-local column metadata. Standard SQL closure is `POST /_plugins/_sql/close`; verify Serverless closure independently against its supported API matrix. PPL `fetch_size` is not evidence of a continuation cursor. [PIT API](https://docs.opensearch.org/latest/api-reference/search-apis/point-in-time-api/), [SQL API](https://docs.opensearch.org/latest/sql-and-ppl/sql-and-ppl-api/index/), [query limitations](https://docs.opensearch.org/latest/sql-and-ppl/limitation/), [upstream PPL fetch-size semantics](https://github.com/opensearch-project/sql/blob/8087c66c476fdccbbeabfe061690f59e42474e51/docs/user/interfaces/endpoint.rst#L383-L398)
- On query changes, session release, TTL expiry, cache eviction, cancellation, and normal shutdown, release owned remote resources with a short independent cleanup deadline. Record cleanup failure; finite keep-alive limits resource retention after crashes or network failures.
- Keep PIT consistency separate from best-effort `search_after` without PIT. Never silently switch consistency modes after PIT expiry.

## 5. UI scope

Ship `connection-modal.extra_fields` for deployment/auth selection, endpoint-related settings, region, profile, explicit credentials, and limits. Use the host secret API and fail visibly when secure storage is unavailable; do not save plaintext as a compatibility fallback. Hide/relabel standard inputs through documented manifest support where useful. [UI extension reference](https://tabularis.dev/wiki/ui-extensions)

Use the existing editor and prefix syntax for mode selection. Verify Run, Run Selection, Run All, statement splitting, saved queries, and page navigation. No editor-toolbar slot or editor mutation API is assumed.

Use the existing JSON viewer for nested `_source`. If needed, add narrowly scoped row-inspection and index-metadata actions through documented slots and host modals. Query hooks currently execute on the active connection; any slot action must check that its context connection matches the active connection and prevent cross-connection execution. No direct Tauri calls or DOM manipulation. [Actual query hook](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/packages/web-ui/src/hooks/usePluginApi.ts#L17-L51)

Build one IIFE per component, externalizing React, JSX runtime, and `@tabularis/plugin-api`; package bundles and localization files with the executable. UI is TypeScript/React; the driver implementation remains Go.

## 6. Implementation sequence and acceptance gates

### M1 — Freeze integration and safety feasibility

Deliver `docs/protocol.md`, `docs/query-modes.md`, `docs/compatibility.md`, and bounded spike tests under `tests/integration/`.

1. Select a released Tabularis version with required secret/session APIs; pin source, binary, manifest schema, plugin API package, Go toolchain and OpenSearch client.
2. Capture actual host calls for connection testing, index opening, filters, sorting, paging, cancellation and tab closure. Define wire fixtures from those observations.
3. Verify prefix-bearing DSL/PPL/REST through the actual editor, including splitting and multi-line bodies.
4. Select maintained SQL/PPL parsing components or upstream grammar generation with a Go-compatible runtime. Prove read AST classification, required host SELECT translation, and rejection of side-effecting commands. Record licensing and generation reproducibility.
5. Freeze the deployment feature matrix and the query/result normalization rules, including raw aggregation display.

Exit: minimal real-host round trips pass; required query safety and generated-query handling are demonstrated. Unresolved host or parser limitations remain explicit blockers. Any required host modification or feature-scope change needs a separate scope decision; do not silently replace requested features.

### M2 — Go runtime, transport and secure connections

Implement `cmd/`, `internal/protocol/`, `internal/connection/`, transport/signing, the manifest, and connection UI. Follow useful TDD slices: write tests for the specific behavior, implement, run, and iterate.

Exit: malformed requests and unknown methods return correct protocol errors; large valid frames work within configured limits; concurrent calls do not corrupt stdout; cancellation interrupts a real in-flight operation. Self-hosted TLS/Basic/anonymous and AWS provider modes pass their available integration cases. Two connections with different endpoints/regions/credentials cannot share state. Secret save/edit/clear/restart tests verify no credential values in saved connection JSON or logs.

### M3 — Explorer and document inspection

Implement metadata, mappings, aliases, bounded native document reads, generated SELECT translation, and normalization.

Exit: fixtures cover empty indices, differing document shapes, nested/object/multi-fields, vectors/geo values, missing `_source`, IDs requiring escaping, aliases with multiple targets, restricted metadata permissions, and large numeric values. Index double-click, filters, sorts, configurable page sizes and document lookup work in Tabularis. Native mappings/settings/count/storage metadata are inspectable when permitted; unavailable data is labeled.

### M4 — Query engines and read-only enforcement

Implement SQL/PPL execution, DSL search, aggregations/vector queries, REST operation registry and optional query templates.

Exit: each requested mode runs against supported real backends; SELECT/filter/sort/group/aggregate/PPL pipelines normalize correctly. Every exposed execution path rejects writes and ambiguous syntax before transport. Test mixed statements, comments/quoted delimiters, REST-embedded SQL/PPL, encoded paths, external URLs, unknown commands, training commands and forbidden methods. Backend authorization remains an independent defense. Unsupported and unauthorized outcomes are distinguishable.

### M5 — Pagination, resource lifecycle and resilience

Implement PIT/search-after, supported SQL/PPL cursors, bounded page checkpoints, eviction and cleanup.

Exit: browse beyond the ordinary result window without duplicate/missing rows in a fixed PIT snapshot; verify concurrent identical queries in distinct sessions/connections; changing sort/filter/page size resets ownership. Test previous pages, unavailable jumps, token expiry, session closure, timeout, cancellation, EOF, and process restart. Verify remote resource release where observable and bounded retention otherwise. Retry behavior stays within the request deadline.

### M6 — Deployment matrix, packaging and release

Implement CI, ZIP assembly, install smoke tests, configuration/IAM examples, and the published compatibility matrix.

Exit: all acceptance criteria below have evidence. ZIPs contain `.tabularium`, executable, UI bundles, README, license and notices. Tag/manifest/binary versions agree; checksums and dependency/license checks pass; install and basic execution succeed on native CI runners. Publish only tested claims; pending AWS evidence blocks an unconditional AWS-support release claim.

Dependencies: M1 → M2 → M3/M4 → M5 → M6. After M1, independent UI and backend implementation can run in parallel with explicit file ownership; integration and release verification remain leader-owned.

## 7. Test and release acceptance criteria

1. **Protocol:** real subprocess stdio tests cover request IDs, nulls, invalid JSON/params, notifications, concurrency, bounded frame sizes, cancellation, EOF and stdout purity.
2. **Authentication:** prove Basic/anonymous/TLS/custom CA; AWS default chain, named profile, SSO profile, explicit temporary credentials and expiration. Test signing service/region and configuration changes. Real AWS cases require authorized pre-existing endpoints and credentials; missing access is an explicit validation gap.
3. **Isolation:** simultaneous connections spanning two regions/accounts where available, plus multiple sessions on one endpoint, never mix credentials, metadata, or page state.
4. **Metadata/documents:** index listing, aliases, nested mappings, specialized types, scalar grids, JSON inspection, ID lookup, filters, sorting and page sizes pass the documented fixture suite.
5. **Queries:** SQL, PPL and DSL demonstrate filters/sorts/aggregations and compatible vector searches. Raw structured results remain inspectable. Record backend-specific unsupported operations.
6. **Safety:** all exposed paths fail closed for data/configuration mutation. Run security assertions against request classification and authorized read-only backends; do not test destructive requests against formal data sources.
7. **Pagination:** bounded first pages and deep PIT traversal pass correctness, expiry, isolation and cleanup tests. Verify cursor behavior independently for each supported language/deployment.
8. **Host UX:** connection create/edit/restart, keychain secrets, explorer, filter/sort, mode prefixes, JSON views, split-view isolation, timeout and tab closure work in the selected released Tabularis version.
9. **Compatibility:** record exact OpenSearch image tags/digests, security configuration, client/toolchain/host versions, AWS region and collection type, API results and test date. Include a chosen 2.x release, a chosen 3.x release, managed domains, and all three Serverless collection types. Test an early 2.x boundary separately before claiming the entire major line.
10. **Build quality:** `gofmt` check, `go vet ./...`, `go test -race ./...`, configured static analysis and vulnerability checks pass. UI typecheck, unit tests, lint and production bundle checks pass. Real integration suites are required in addition to pure parser/normalizer tests; fixtures alone do not establish connectivity.
11. **Distribution:** proposed targets are Linux amd64/arm64, macOS amd64/arm64 and Windows amd64. Validate registry platform labels and installation for each; cross-compilation alone is not runtime verification. Expand architecture support only with packaging/runtime evidence.
12. **Documentation:** installation, connection examples, CA configuration, SSO login prerequisites, credential refresh limits, read-only IAM/data-access examples, pagination limits, troubleshooting and version matrix are complete. AWS policies are documentation only; the plugin does not provision or change infrastructure.

Docker is available locally (server 28.5.1), but no container or database was started for this planning task. Future local verification should use project-scoped disposable containers and preloaded fixtures. Establish authorization for fixture population before any database writes; all formal-source verification remains read-only. No Kubernetes changes are part of the work.

## 8. Risks and release blockers

- **Parser coverage:** safe full-query validation and host SQL translation need demonstrated parser compatibility. M1 blocks downstream query implementation until resolved.
- **Host integration:** release-specific splitting, session forwarding, secret storage and result display need actual application evidence. Use documented APIs only; any host extension is a separately approved scope decision.
- **Backend variability:** SQL/PPL, metadata, PIT and cursor support vary by deployment and permissions. Keep per-operation evidence and explicit unsupported/unauthorized states.
- **Resource retention:** abrupt termination cannot guarantee immediate remote cleanup. Short bounded keep-alives, ownership tracking, TTL eviction and normal-close tests limit retention.
- **Precision/structure:** Go and the host JavaScript UI have different numeric representations. Preserve original JSON and test values beyond JavaScript's exact integer range.
- **AWS access:** local Docker cannot prove IAM signing, SSO refresh, managed-domain authorization, or Serverless policies. Required live evidence cannot be replaced with emulation.
- **Release identity/license:** confirm owner and repository URLs before publishing; verify third-party notices and generated-parser licensing.

Planning stop condition: this evidence-backed sequence and its acceptance gates are saved and reviewed. Implementation stop condition: all requested feature areas meet their test gates, release claims match recorded evidence, and any remaining platform limitations are explicit.
