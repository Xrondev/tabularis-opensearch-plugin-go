# Tabularis OpenSearch Plugin — v0.1.0 specification

Status: proposed specification for implementation. No implementation or deployment compatibility is certified by this document.

Companion documents: [implementation plan](implementation-plan.md) and [implementation ticket OS-001](tickets/001-opensearch-driver-v0.1.0.md).

## 1. Purpose and authority

Provide a read-oriented OpenSearch driver implemented in Go, with secure connections, index exploration, document inspection, native queries, and bounded pagination inside Tabularis.

This specification defines intended product behavior. The implementation plan contains the research baseline and source snapshots. Requirement IDs in this document provide acceptance-test traceability. Defaults and plugin-owned fields below are proposed design values, not fields claimed to exist in Tabularis or OpenSearch APIs.

Before implementation proceeds beyond the compatibility spike, resolve the G1/G2 feasibility decisions needed for the affected work: released-host integration, query parsing, browse translation and API selection. Complete G1/G2 feature evidence during implementation, G3 with metadata/query acceptance, G4 with pagination/resource acceptance, and G5 before release. A gate may refine an implementation detail with evidence; it must not silently remove a requested capability or authorize a host change.

### Supported deployment targets

- Self-hosted OpenSearch 2.x and 3.x, with exact tested releases recorded.
- Amazon OpenSearch Service managed domains.
- Amazon OpenSearch Serverless collection endpoints: search, time-series and vector-search, with collection generation/type recorded separately.

Support means that the operation has been verified for the recorded deployment, permissions and version. SQL/PPL availability, pagination, metadata and vector operations are evaluated independently. PIT is version-dependent and starts at OpenSearch 2.4. [PIT reference](https://docs.opensearch.org/latest/api-reference/search-apis/point-in-time-api/)

### Scope

The release includes connection creation/testing, secure credential storage, metadata, alias inspection, document browsing and ID lookup, SQL/PPL/DSL queries, safe REST inspection, pagination, cancellation, UI integration, tests, documentation and distribution.

Document/index mutation, ingestion, cluster administration, infrastructure provisioning, dashboards and a full custom editor are outside the release. Transient PIT/cursor creation and closure are permitted only for owned read sessions. No formal data-source writes or Kubernetes deployment changes are authorized.

## 2. User workflows

1. **Connect:** select deployment and authentication, enter endpoint and relevant settings, test, save, restart Tabularis, and reconnect without exposing saved secrets.
2. **Explore:** list permitted indices, search/filter the list, inspect native mappings, fields, aliases, settings, counts and available storage information.
3. **Browse:** open an index, inspect scalar columns and original JSON, apply supported filters/sorts, change page size, move through pages, and retrieve a document by ID.
4. **Query:** select a mode explicitly in the existing editor, run SQL/PPL/DSL or an allowed REST read, and inspect tabular and structured output.
5. **Work concurrently:** use separate connections, regions/accounts and editor sessions without shared credential, result, capability or cursor state.
6. **Handle failure:** receive a bounded, actionable error for invalid configuration, unavailable credentials, permission failures, incompatible APIs, expired pagination, partial search results or cancellation.

## 3. Connection model

### CONN-01 — Configuration representation

The proposed canonical endpoint is `params.extra.endpoint`. It is a full HTTP(S) origin, optionally with an explicitly supported reverse-proxy base path. The plugin does not construct a second endpoint from hidden host/port values. The UI hides host/port/database inputs through documented presentation overrides; username/password remain available only for Basic authentication.

Plugin-owned non-secret keys in the host's `extra` string map:

| Key | Values and validation |
| --- | --- |
| `endpoint` | Required absolute HTTP(S) URL; reject userinfo, query, fragment, invalid port and unsupported schemes. Preserve the validated base path. |
| `deployment` | Required: `self_hosted`, `aws_managed`, `aws_serverless`. |
| `auth_mode` | Required: `anonymous`, `basic`, `aws_default`, `aws_profile`, `aws_static`. |
| `aws_region` | Required for AWS signing; explicit nonempty region; no silent cross-region retry. |
| `aws_profile` | Required only for `aws_profile`; passed to the SDK profile selector. |
| `index_pattern` | Optional bounded index-selection pattern; limits discovery scope, not an authorization boundary. Default `*`. |
| `request_timeout_ms` | Decimal integer; default 30000, permitted 1000–300000. |
| `page_size` | Decimal integer; default 100, permitted 1–1000. |
| `pagination_mode` | `offset` (default) or `pit`; PIT uses a verified single-concrete-index, one-primary-shard search and can require additional permissions and server memory. |
| `verified_exact_fields` | Optional comma-separated, unique top-level field names. The user attests that each field is single-valued, has no `null_value` or normalizer, and has equivalent SQL/DSL filtering and sorting semantics. Every non-null value must be visible to `exists`, including values affected by `ignore_above`, `ignore_malformed`, and indexing settings. Applies only to a concrete index; unsupported or ambiguous mappings are rejected. Requires index resolution and settings permissions. UUID checks bracket the mapping and search; index identity and mappings must remain stable during queries, since concurrent changes cannot always be detected. |

Secret keys delivered through `params.extra` after host resolution: `aws_access_key_id`, `aws_secret_access_key`, `aws_session_token`. Basic authentication uses native `username`/`password`. Custom CA uses native `ssl_ca`; its path/content representation must match the selected host and be frozen by gate G1. The driver does not invent a second secret store. [Host connection model](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L186-L313)

Reject malformed values before network activity. Ignore unrelated host connection fields, but reject conflicting plugin-owned authentication settings. Never execute a supplied startup script. Future plugin-owned keys require documented semantics and tests.

### CONN-02 — Authentication combinations

| Deployment | Accepted modes |
| --- | --- |
| Self-hosted | `anonymous`, `basic` |
| AWS managed | `aws_default`, `aws_profile`, `aws_static`, explicitly configured `basic` |
| AWS Serverless | `aws_default`, `aws_profile`, `aws_static` |

Basic authentication requires username and password. Static AWS authentication requires access key and secret; session token is optional. Profile mode requires a profile and excludes static key fields. Default-chain mode excludes explicit profile/static fields. Switching modes clears inactive secrets through the host secret API after the user saves the form.

Managed SigV4 uses service `es`; Serverless uses `aoss`. AWS signing requires HTTPS, the configured region and the exact request payload. Verify Serverless payload hashing through integration tests. [Official Go client](https://docs.opensearch.org/latest/clients/go/), [Serverless clients](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-clients.html)

### CONN-03 — Credential lifecycle

Use AWS SDK v2 providers and refresh-aware credential caching per connection configuration. Named SSO profiles rely on the SDK/CLI login state; unavailable login state produces an actionable error. Static credentials do not acquire refresh behavior. No automatic browser login or fallback to another credential mode occurs after a selected provider fails. [AWS SDK configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html)

Do not change process-wide AWS environment variables to select a connection. Provider failures and expired credentials must not affect unrelated connections.

### CONN-04 — Persistence and secret handling

Use `setSecretField` for all explicit AWS credential material. Non-secret settings use `setExtraField`. Saved secrets are represented in the UI by stored/not-stored state; existing secret values are not returned to the extension. Secret clearing, editing and duplication follow host behavior. [Host secret implementation](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugin_secrets.rs#L1-L9)

When secure persistence is unavailable, explicit-credential save fails with an actionable message. No plaintext fallback is allowed. Raw connection parameters, authorization headers, tokens, credential-bearing URLs and response bodies are excluded from logs.

### CONN-05 — TLS and transport

TLS verifies server identity using system roots and an optional custom CA. Invalid certificates fail. No automatic downgrade or verification bypass is provided. HTTP is allowed only for an explicitly configured self-hosted endpoint. The connection form warns when Basic credentials would use HTTP.

Use connection-specific reusable transports. Disable redirects and automatic node discovery for fixed endpoints. Requests remain under the configured origin/base path. Retry only registered retryable read operations, with no more than two retries, jitter, and the original deadline; do not retry authentication/authorization failures as transient errors. Resource creation is not retried when its outcome is unknown.

### CONN-06 — Isolation and connection testing

Key retained clients by connection ID and a private configuration generation/fingerprint. A configuration or credential change invalidates capabilities and pagination associated with that generation. Ad-hoc requests lacking a connection ID use request-scoped clients and cannot own persistent pagination state.

`test_connection` performs a genuine permitted read within the configured index scope. It does not require cluster health or AWS control-plane listing. Distinguish successful scoped read access from permission to list every index. `ping` performs a bounded compatible read and does not rebuild all metadata.

## 4. Runtime protocol

### RPC-01 — Framing and concurrency

The executable runs JSON-RPC 2.0 over newline-delimited stdin/stdout. JSON encoding uses the Go standard library. Stdout contains only responses; stderr contains sanitized diagnostics. Preserve string/numeric request IDs without converting them through floating point.

Keep reading input while requests execute so cancellation can be processed. Serialize response writes. Bound concurrency, queued work, input frames and response bytes. A broken stdout terminates the process; do not continue reporting successful work when results cannot be delivered.

### RPC-02 — Required methods

| Method | Result/behavior |
| --- | --- |
| `initialize` | Accept validated non-secret defaults; return an empty object on success. |
| `test_connection` | Return `{"success":true}` only after a successful read. |
| `ping` | Return null on a successful lightweight read. |
| `get_databases` | Return `["default"]` for the single implicit database. |
| `get_schemas` | Return `[]`; schemas are disabled. |
| `get_tables` | Return index names as table metadata, with optional comments. |
| `get_columns` | Return documented host column metadata. |
| `get_foreign_keys`, `get_indexes`, `get_views`, `get_routines` | Return `[]` for inapplicable relational objects. |
| `execute_query` | Execute one validated query and return a host QueryResult. |
| `release_session` | Release resources owned by the supplied session; repeated release is safe. |
| `cancel` | Notification only; cancel original request identified by `params.id`; no response. |

The normal query arguments are `params`, `query`, `limit`, `page`, `schema`, and optional `session_id`. Connection parameters are nested under the request's `params.params`. Pages are one-based; invalid zero/negative limits/pages fail. A null limit uses the configured page size, not unlimited retrieval. Nonempty schema values are rejected for this schema-less driver. [Host dispatch](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugins/driver.rs#L1235-L1403)

`execute_query_batch` may remain unimplemented because the inspected host supports per-query fallback; each fallback invocation must pass the same safety checks. A later batch implementation must validate each statement independently. Optional capability discovery is gated on the selected released host and registry schema.

### RPC-03 — Query result shape

```json
{
  "jsonrpc": "2.0",
  "id": 42,
  "result": {
    "columns": ["_index", "_id", "_score", "_source"],
    "rows": [["events", "event-1", null, {"status": "ready"}]],
    "affected_rows": 0,
    "truncated": false,
    "pagination": {
      "page": 1,
      "page_size": 100,
      "total_rows": null,
      "has_more": false
    }
  }
}
```

Rows are rectangular and aligned to columns. Unknown totals remain null. A lower-bound OpenSearch hit count is not an exact total. `has_more` requires backend continuation evidence or a bounded look-ahead; page fullness alone is insufficient. `truncated` records a deliberate finite result cap, not ordinary pagination. [Host result types](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L532-L559)

### RPC-04 — Errors and cancellation

Use JSON-RPC standard errors: parse `-32700`, invalid request `-32600`, unknown method `-32601`, invalid parameters `-32602`, execution/internal failure `-32603`. Unknown methods include `Method not found` in the message. Mutation methods return an explicit read-only rejection before transport.

Error messages identify configuration, TLS/network, credential, permission, invalid query, unsupported operation, expired page state, cancellation or backend failure. These are plugin diagnostic categories, not invented backend status codes. Preserve sanitized native status/type/request ID when available. The host must receive a useful message even if it ignores optional error data.

Cancellation stops local work and the HTTP request through its context, removes queued work where applicable, and closes owned resources with a separate bounded cleanup context. It does not claim that an HTTP disconnect proves remote execution has stopped. Duplicate/late cancel notifications are ignored. EOF initiates bounded shutdown and cleanup.

## 5. Explorer and document metadata

### META-01 — Index discovery

Map OpenSearch indices to host tables. List only the configured scope and permitted resources. Do not require all-cluster access. Support index-name searching/filtering through the host explorer and explicit index-pattern configuration.

Load expensive counts/storage metadata lazily, not once per index on every explorer refresh. Missing values remain unavailable. If the backend can search a known index but cannot enumerate indices, explain the discovery restriction and allow explicit scoped configuration.

### META-02 — Mapping traversal

Retain native types and traverse nested objects, multi-fields and specialized mappings. Distinguish an object boundary from a nested boundary and a source field from a multi-field. Preserve full mapping JSON for inspection.

Host columns contain `name`, `data_type`, `is_pk`, `is_nullable` and `is_auto_increment`; optional comments/full type retain native detail. Do not treat OpenSearch indexed fields as relational secondary indexes. `_id` is not globally unique across multiple indices. Native JSON-compatible host types can be used for object/array rendering while native mapping type remains available. [Host metadata types](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/models.rs#L469-L529)

### META-03 — Aliases and inspection

Expose alias names, target indices and returned filters/routing metadata without replacing alias searches with unrestricted target-index searches. Inspect settings, counts, field capabilities and available storage statistics through registered read operations. Alias and metadata permission failures remain visible and do not silently change the query target.

### META-04 — Documents

Preserve `_index`, `_id`, `_score` and complete `_source` where provided. Render scalar fields alongside original JSON. Use a stable reserved metadata namespace and encode source paths injectively so dotted names and nested paths cannot collide; freeze the display/path encoding in G3.

Determine page columns from mapping metadata plus all documents in the bounded page. Keep arrays and objects structured. Missing cells may display null, but the original source preserves whether a field existed. A document with `_source` disabled remains inspectable through available metadata/fields without inventing source content.

Document lookup supports an encoded ID and optional routing. A missing document is reported as not found; an authorization failure is not treated as absence.

## 6. Query behavior

### QUERY-01 — Modes

Recognize an optional exact mode directive on the first nonblank line: `#!sql`, `#!ppl`, `#!dsl`, `#!rest`. Reject unknown directives. Trim only envelope whitespace; preserve the query body sent to a native language API after validation.

Without a directive, parse as SQL. A query in the verified browse subset uses the native-search adapter; other validated read SQL uses the SQL endpoint. Explicit `#!sql` always requests native SQL. Identical text is routed identically from table and editor paths because host requests carry no origin flag.

Mode execution through Run, Run Selection, Run All, saved queries and pagination is a G1 acceptance gate. No unsupported editor-extension API is assumed.

### QUERY-02 — Native SQL and PPL

SQL supports the verified read grammar, including SELECT, filtering, sorting, grouping and aggregations. PPL supports the verified read pipeline, field selection, filtering, sorting, statistics and aggregation. A parser must classify the entire statement/pipeline, including subqueries and expressions, before transport. Unknown or side-effecting forms fail closed.

Normalize verified native response payloads into column/row output. Preserve server column order. Cache schema only within the owning continuation session when subsequent cursor responses omit it. No textual SQL rewriting or pagination suffix concatenation is allowed without parser-backed correctness tests.

If the backend lacks a requested language/API, return an unsupported-operation error. Do not translate arbitrary SQL/PPL into an unverified approximation.

### QUERY-03 — Browse adapter

The adapter must cover the host's verified index-opening SELECT, selected scalar fields, COUNT, WHERE, ORDER BY and finite LIMIT forms. Its required initial expression coverage includes boolean conjunction/disjunction, comparisons, IN, null tests and parentheses for mappings where equivalent native-search semantics can be demonstrated.

Resolve fields against mappings. Do not silently replace a text field with a `.keyword` field. Reject ambiguous multi-index field types. Nested-field predicates require semantics preserving nested-document association. Null tests require explicit treatment of missing values and mapping `null_value`; route to native SQL or report unsupported when equivalence is not established.

Document exact supported expressions and type rules. Test the same query in table and editor paths, including null/missing fields, dates, escaped identifiers, text/keyword fields, nested values and ordering. Unsupported adapter forms may use native SQL only through the documented routing rule and after full safety validation.

### QUERY-04 — DSL envelope

Proposed plugin-owned syntax:

```text
#!dsl
{
  "index": "events",
  "body": {
    "query": {"term": {"status.keyword": "ready"}},
    "sort": [{"created_at": "desc"}]
  }
}
```

`index` is a required nonempty index/alias/pattern string within configured scope; `body` is a required native search JSON object. These envelope fields are defined by this plugin, not by the host. Parse with `encoding/json`; reject duplicate/unknown envelope fields and trailing JSON values. Use maintained JSON tooling if needed for duplicate-key detection rather than implementing a JSON parser.

Support native search/filter/sort/aggregation and compatible vector queries. Preserve body semantics; enforce bounded `size` and response limits. Document precedence: a supplied body `size` of 1–1000 determines the requested page size; an omitted size uses RPC limit/configuration. Explicit `size:0` is allowed for aggregation/diagnostic-only searches, returns structured output with `pagination:null`, and does not start a document pagination session. Reject negative, non-integer or oversized values. Reject conflicting user-supplied `from`, `search_after`, or PIT state with plugin-managed continuation. User-owned PIT IDs are not admitted into the managed resource registry.

### QUERY-05 — REST envelope

Proposed syntax:

```text
#!rest
POST /events/_search
{"query":{"match_all":{}},"size":25}
```

Parse the method/path line as plugin syntax and the optional body as JSON. Resolve paths beneath the configured endpoint base path. Reject absolute URLs, fragments, encoded traversal and unsupported query parameters. GET is not automatically safe; every method/path/body combination must match a registered operation.

Search responses use document normalization. Metadata/count/explain responses retain structured JSON. REST requests to SQL/PPL endpoints use the same grammar policy as their dedicated modes. User REST requests cannot create or close arbitrary pagination resources.

### QUERY-06 — Structured output and precision

Represent metadata/aggregation-only output as an inspectable JSON cell in a `response` column. For searches containing both hits and aggregations, retain document rows and expose the complete structured aggregation/diagnostic payload through a host-supported result surface verified in G1. Use `additional_results` only when the selected host renders it. This is an acceptance gate, not permission to omit aggregations.

Decode numbers without float64 conversion. Values outside the host JavaScript exact-integer range must have a lossless textual representation available; G3 freezes grid display and original-JSON access so neither silently rounds values. Preserve arrays, objects, nulls and empty results.

Timed-out searches or failed shards cannot appear as complete success. Return an explicit incomplete-result error with sanitized diagnostics unless a verified host presentation can visibly mark partial rows. Never hide partial status in a field the host does not render.

## 7. Read-only enforcement

### SAFE-01 — Layered enforcement

Declare a read-only manifest, reject mutation RPCs, enforce parsed SQL/PPL read policies, enforce registered REST operations and use least-privilege credentials. These protections apply to every input path, including native editor input, UI actions and batch fallback.

SQL/PPL are parsed with maintained components or generated upstream grammars. No hand-written mature-language parser, first-word classification or regex-only policy is acceptable. SQL DELETE and PPL ML training are explicit adversarial cases. [SQL DELETE](https://docs.opensearch.org/latest/sql-and-ppl/sql/delete/), [PPL ML](https://observability.opensearch.org/docs/ppl/commands/ml/)

### SAFE-02 — Operation registry

Each registered operation specifies an exact method/path template, permitted parameters/body, resource effect, deployment support and response decoder. The required read families are search, count, document lookup, mappings, settings, field capabilities, aliases, index listing/statistics and selected explain operations. SQL/PPL reads require grammar validation in addition to route validation.

Freeze the concrete endpoint registry against official documentation and selected versions in G2. No arbitrary pass-through, bulk mutation, administrative paths or wildcard cleanup is admitted. URL normalization happens before policy matching and again at final destination construction; encoded values cannot escape a path segment or the configured base path.

### SAFE-03 — Permissions and data handling

AWS documentation separates IAM data-plane access, network policy and Serverless data-access policy. Test least-privilege API access without requiring Dashboards. Do not equate a forbidden response with API absence. [Serverless supported operations](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-genref.html), [IAM](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/security-iam-serverless.html), [data access](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-data-access.html)

Document bodies, query text and raw backend response bodies are not logged or persisted as runtime cache files. Sanitized test evidence must exclude credentials and private document contents. Public documentation uses synthetic examples only.

## 8. Pagination and resources

### PAGE-01 — Ordinary pages

Use bounded remote retrieval; never download a whole index to slice local pages. Respect backend result-window limits and configured page/byte limits. Unknown counts remain unknown. Queries that cannot continue must report an accurate terminal/capped state.

### PAGE-02 — Deep pages

Use PIT plus `search_after` only when supported and authorized, with an exact stable sort/tie-breaker verified against the backend. Preserve returned sort values without precision loss. No session means no retained remote cursor: support bounded stateless pages and return an actionable limitation for deep requests.

Bind each retained state to connection generation, session ID, query/mode/index fingerprint, page size and sort. A session ID already bound to another connection generation must not be reassigned while active. Keep a session-to-owner index because host `release_session` carries only the session ID. [Host release behavior](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/src-tauri/src/plugins/driver.rs#L1387-L1403)

### PAGE-03 — Navigation and consistency

Page 1 starts a new query generation. Sequential next pages advance from the owned checkpoint. PIT-backed previous pages use retained checkpoints while that PIT remains valid; bounded offset queries can refetch a previous page. Native cursors support previous pages only when replay has been independently verified; otherwise return an explicit restart instruction. Uncached jumps fail with a restart/navigation instruction rather than unbounded replay. Filter/sort/page-size changes invalidate continuation.

Exhausting search hits sets `has_more:false` but retains an owned PIT until session release, replacement or idle expiry so retained checkpoints remain usable. Exhausted native cursors are closed and cannot be reused. A new page-1 request always begins a new generation rather than replaying an old checkpoint.

Serialize continuation advancement within a query generation. A late response from an invalidated generation cannot overwrite current state. PIT expiry fails explicitly; do not silently resume against a changed dataset. A new explicit page-1 request starts a new snapshot.

### PAGE-04 — Native cursors

Use native SQL/PPL continuation only where verified for that backend and query form. PPL `fetch_size` is not proof of a cursor. Retain schema across schema-less SQL continuation pages; a terminal response ends continuation. Verify closure independently on Serverless. [SQL/PPL API](https://docs.opensearch.org/latest/sql-and-ppl/sql-and-ppl-api/index/), [limitations](https://docs.opensearch.org/latest/sql-and-ppl/limitation/), [PPL source documentation](https://github.com/opensearch-project/sql/blob/8087c66c476fdccbbeabfe061690f59e42474e51/docs/user/interfaces/endpoint.rst#L383-L398)

### PAGE-05 — Cleanup

Close only resources recorded as owned. Cleanup runs on native-cursor exhaustion, explicit session completion/release, cancellation, query replacement, TTL expiry, cache eviction, connection generation change and normal shutdown. Hit exhaustion retains the PIT only as specified in PAGE-03 and never beyond its idle expiry. Cleanup uses a separate short deadline and emits sanitized failure diagnostics. Finite server keep-alives bound orphan retention after crashes/network loss; instant crash cleanup is not promised.

## 9. UI behavior

### UI-01 — Connection fields

Implement `connection-modal.extra_fields`, filtered to the OpenSearch driver. Render deployment/auth choices, endpoint, region/profile, credential input, index scope and request/page settings. Validate incompatible combinations immediately and in Go. Store secrets through `setSecretField`; do not expose them through global settings. Explain expired SSO credentials without launching external login automatically. [UI API](https://tabularis.dev/wiki/ui-extensions)

### UI-02 — Inspection and mode selection

Use mode directives in the existing editor. Show nested documents through the host JSON viewer. Optional scoped metadata/row actions may use documented toolbar/row slots and modals; they do not replace the editor.

Every query-producing slot checks that its context connection matches the active query connection and disables execution otherwise. Recheck at invocation, not only render time. No direct Tauri commands or out-of-subtree DOM access. [Host query hook](https://github.com/TabularisDB/tabularis/blob/92820f0df1fc5ff826ec03bec5a8119ae1d73c14/packages/web-ui/src/hooks/usePluginApi.ts#L17-L51)

### UI-03 — Bundles and usability

Ship one IIFE bundle per component; externalize React, JSX runtime and `@tabularis/plugin-api`. Use host theme tokens, labeled inputs, keyboard operation and localized strings. A bundle failure must not prevent normal driver RPC operation. Validate bundle loading from the actual packaged plugin.

## 10. Proposed operational limits

These initial values are product defaults to verify in G4, not measured performance claims:

| Resource | Initial bound |
| --- | --- |
| Active requests | 16 per process, 4 per connection |
| Queued executable requests | 64 per process; cancellation remains readable |
| Input JSON-RPC frame | 8 MiB |
| Decoded backend response / emitted response | 32 MiB each |
| Retained connection clients | 16; idle eviction after 15 minutes |
| Retained pagination sessions | 32 per process |
| Retained checkpoints | 32 per session; no full document-page cache |
| PIT keep-alive | 60 seconds, renewed only by active owned reads |
| Local pagination idle expiry | 60 seconds |
| Cleanup deadline | 5 seconds |
| Normal shutdown budget | 10 seconds |

Capacity exhaustion returns an explicit bounded error; it does not silently discard an active session. Evict idle state first and close owned resources. Byte caps apply after decompression. A request deadline covers credential retrieval, retries, transfer and normalization. Host timeout may be shorter and is honored through cancellation.

## 11. Implementation boundaries

Use the module layout in the implementation plan. Protocol DTOs do not leak into transport adapters; result normalization does not execute requests; the safety layer authorizes every outgoing operation; the pagination layer owns cursor/PIT identity and cleanup.

Use the official OpenSearch Go client and AWS SDK v2. Pin exact versions/toolchain after G2. The researched client tag v5.1.3 requires Go 1.26.0; the research environment had Go 1.25.4. [Pinned client module](https://github.com/opensearch-project/opensearch-go/blob/v5.1.3/go.mod)

Capability state is per connection generation and operation: supported, unsupported, unauthorized or unknown. Probe lazily with bounded reads and avoid repeated probes after a deterministic outcome; connection edits/retests invalidate relevant observations. Do not infer language availability solely from a server version or a failed request.

## 12. Release requirements

### RELEASE-01 — Manifest and identity

Proposed driver ID `opensearch`, display name `OpenSearch`, executable `opensearch-plugin`, version `0.1.0`. Use canonical `.tabularium`, read-only/single-database capabilities and disabled relational mutation surfaces. Set minimum runtime version only after testing a released host. Apache-2.0 and public repository ownership require confirmation before publication.

### RELEASE-02 — Artifacts

Package Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 ZIPs with executable, `.tabularium`, UI bundles, localization, README, license and third-party notices. Validate registry platform labels, executable paths/permissions, archive layout and checksums. Tag, manifest and binary versions agree. Native runner installation/smoke evidence is required for each claimed platform.

### RELEASE-03 — Compatibility and docs

Publish a matrix of exact host/client/Go/backend versions, image digests, deployment/collection types, authentication modes, supported operations and known limitations, with test dates. Include self-hosted 2.x/3.x, managed domains and each Serverless collection type. Record early-2.x limitations explicitly rather than implying all 2.x features are identical.

Provide installation, connection/auth examples, custom CA setup, SSO prerequisites, least-privilege AWS policy examples, query syntax, filters, JSON inspection, pagination limitations, cancellation and troubleshooting. Policies are examples, not provisioning actions.

## 13. Acceptance and evidence matrix

| Test ID | Requirements | Required evidence |
| --- | --- | --- |
| AT-01 | CONN-01–06 | Actual connections for each claimed auth/deployment combination; invalid settings fail before transport; expired credentials fail clearly. |
| AT-02 | CONN-03–06, PAGE-02 | Concurrent endpoints/accounts/regions/profiles and connection edits show no credential, capability or cursor leakage. |
| AT-03 | CONN-04, UI-01 | Save/edit/clear/restart tests; saved JSON and sanitized diagnostics contain no secret values; unavailable secure storage blocks explicit-secret save. |
| AT-04 | RPC-01–04 | Real executable stdio tests for valid/invalid frames, request IDs, notifications, concurrent calls, output integrity, cancellation and EOF. |
| AT-05 | META-01–04 | Empty/mixed-shape/nested/multi-field/vector/geo/missing-source fixtures; aliases and permission-restricted metadata; escaped IDs and routing. |
| AT-06 | QUERY-01–03 | Actual host open/filter/sort/count/query runs; parser coverage; identical text routes identically; null/text/nested semantics are documented and verified. |
| AT-07 | QUERY-02,04–06 | Real SQL/PPL/DSL reads, aggregation/vector examples where supported, JSON inspection and lossless large-number access. |
| AT-08 | SAFE-01–03, QUERY-05 | Adversarial grammar/path/body corpus produces no unauthorized outbound operation; backend read-only permissions independently verified without production mutation attempts. |
| AT-09 | PAGE-01–05 | Traversal beyond ordinary window in fixed snapshot, no duplicate/missing fixture IDs, previous-page/jump behavior, expiry and cleanup evidence. |
| AT-10 | RPC-04, section 10 | Real slow/in-flight cancellation, deadline/retry bounds, queue/byte/session limits, abrupt process exit and TTL retention checks. |
| AT-11 | UI-01–03 | Released-host connection flow, mode directives, saved queries, split-view isolation, tab closure and packaged UI loading. |
| AT-12 | RELEASE-01–03 | Go format/vet/race tests/static analysis/vulnerability scan; UI lint/typecheck/tests/build; native artifact installation and version/schema checks. |

Each test record includes environment versions, command or UI steps, expected result, actual result, pass/fail, and a sanitized evidence location. Skipped/unavailable tests are not passes. Pure unit tests establish parser/normalizer behavior; subprocess, real backend and actual-host tests establish integration behavior.

Local integration uses project-scoped Docker resources and preloaded fixtures. Fixture population requires appropriate authorization. Formal sources are read-only. AWS validation requires existing authorized endpoints and credentials; local containers do not establish AWS compatibility.

## 14. Decision gates and completion

| Gate | Evidence required | Blocks |
| --- | --- | --- |
| G1 — Released host | Actual RPC capture, secret API persistence, SSL field representation, mode splitting, session lifecycle, structured result rendering and package install. | Final wire/UI behavior and host minimum version. |
| G2 — Query safety and APIs | Maintained parser or reproducible upstream grammar generation, read-only classification, browse translation, exact operation registry, dependency versions/licenses and AWS signing tests. | Production query execution. |
| G3 — Data semantics | Column/path encoding, large-number representation, mapping/filter semantics, aggregation display and alias preservation verified end to end. | Metadata/query acceptance. |
| G4 — Resources | Pagination correctness and cleanup, concurrency/byte/TTL limits, retry and cancellation evidence. | Deep pagination and operational claims. |
| G5 — Deployment/release | Live AWS matrix, platform smoke tests, license/owner confirmation, complete documentation and checksums. | Publication and support claims. |

Implementation is complete when AT-01 through AT-12 pass for the declared support matrix, all gates are closed with evidence, and the ticket's definition of done is satisfied. Any required host modification, missing requested capability or untestable deployment claim is escalated as a specific scope/release decision.
