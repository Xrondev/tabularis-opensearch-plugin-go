# OS-001 — Implement the Tabularis OpenSearch driver v0.1.0

Type: implementation epic with six dependent work packages.

Tracking: local ticket; not published to an external issue tracker.

Target: `tabularis-opensearch-plugin-go`, release `0.1.0`.

Specification: [OpenSearch driver specification](../specification.md).

Planning evidence: [implementation plan](../implementation-plan.md).

This ticket describes required work and evidence. No implementation, integration test, compatibility result, or release is represented as complete.

## Objective

Deliver a Go-based, read-only Tabularis driver for self-hosted OpenSearch, Amazon OpenSearch Service managed domains, and Amazon OpenSearch Serverless collection endpoints. Users can configure independent connections, browse indices and mappings, inspect documents, and run supported SQL, PPL, Query DSL, and allowlisted REST reads through Tabularis.

## Scope

- Self-hosted OpenSearch 2.x/3.x compatibility, qualified by exact tested releases and operation availability.
- HTTP/HTTPS, anonymous and Basic authentication, custom CA trust, AWS SigV4, SDK credential providers, named/SSO profiles, and explicit temporary credentials.
- Connection-local client, credential, capability, and pagination state.
- Index/alias discovery, mappings/settings/count/storage inspection where supported and permitted.
- Document grids, nested JSON inspection, document ID lookup, filtering, sorting, and configurable page size.
- Native SQL, PPL, DSL search/aggregation/vector queries, with parser-backed read-only validation.
- Method/path/body allowlisted REST inspection.
- Bounded pagination, PIT/search-after, verified native cursors, cancellation, expiry and cleanup.
- JSON-RPC over stdio, secure connection UI fields, existing editor integration, platform packages, CI, and operational documentation.

Document/index writes, administration, ingestion, infrastructure provisioning, dashboards, and a custom query editor are outside this ticket. Transient PIT/cursor lifecycle operations are limited to resources owned by the driver.

## Execution boundaries

- Do not change Kubernetes deployments or configuration. Any Kubernetes inspection uses an explicit context and read-only commands.
- Do not write to formal data sources. AWS validation uses authorized, pre-existing endpoints and read-only credentials.
- Use disposable project-scoped Docker environments for local verification. Obtain explicit authorization for fixture population before database writes; the driver itself remains read-only.
- Use maintained parsers or upstream-generated grammars for SQL/PPL. Unknown or unclassified syntax fails closed.
- Preserve scope when a feasibility gate fails. Record the blocker and the decision required; do not silently remove requested functionality or add host changes.
- Use current source contents, small reviewable changes, useful regression tests, and fresh verification evidence for each work package.

## Dependency sequence

OS-001.1 → OS-001.2 → OS-001.3 and OS-001.4 → OS-001.5 → OS-001.6.

Independent UI and backend work may run in parallel after integration decisions are frozen. Assign file ownership explicitly. One integration owner verifies the combined result and release gates.

## OS-001.1 — Validate integration and query safety

**Outcome:** establish an implementable host/protocol/query baseline before downstream feature work.

**Deliverables**

- Pin a released Tabularis build, its source reference, manifest schema and plugin API package.
- Pin a compatible Go toolchain, official OpenSearch Go client and AWS SDK v2 dependencies.
- Capture actual host RPC requests and responses for connection testing, index opening, filtering, sorting, paging, timeout cancellation and session release.
- Define the plugin-owned connection fields, query mode syntax, normalized result representations and resource limits in the specification and focused protocol/query documentation.
- Prove maintained SQL/PPL parser integration, including classification of complete statements/pipelines and translation of the supported browse SELECT subset.
- Establish a deployment/operation compatibility matrix with untested entries explicitly marked.

**Acceptance checklist**

- [ ] A minimal driver completes a real Tabularis connection/query round trip.
- [ ] Existing editor Run, Run Selection, Run All and saved-query paths preserve each query mode and its multi-line body.
- [ ] Exact host pagination/session parameters and result types are captured; no assumed cursor fields are required.
- [ ] Explicit SQL mode selects native SQL; unprefixed browse-subset routing is deterministic in table and editor paths.
- [ ] Parser tests cover permitted reads and rejection of writes, multiple statements, unsupported commands and PPL side effects.
- [ ] Browse semantics cover null/missing fields, text/keyword fields, nested fields and ordering without silent semantic changes.
- [ ] Parser licensing, grammar provenance and reproducible generation are documented.
- [ ] Required secret-storage, session and manifest APIs are demonstrated in the selected released host.

**Evidence:** version pins, sanitized host traces, executable spike tests, parser corpus/results, and a decision record for each unresolved host/parser limitation.

**Blocks:** OS-001.2–OS-001.6 until the required feasibility decisions are resolved. A source checkout alone is not evidence that a released host supports the feature.

## OS-001.2 — Implement runtime and secure connections

**Outcome:** a cancellable Go JSON-RPC runtime with isolated, authenticated OpenSearch clients and a usable connection form.

**Deliverables**

- `cmd/opensearch-plugin/`, protocol types/dispatch, bounded concurrent processing, stdout serialization and shutdown handling.
- Connection configuration validation, HTTP/TLS transport, AWS signing/provider selection, bounded client cache and configuration invalidation.
- Canonical `.tabularium` manifest with read-only and implemented capabilities.
- Connection UI with non-secret settings and host keychain-backed credential fields.

**Acceptance checklist**

- [ ] Real subprocess tests verify request IDs, invalid JSON/params, unknown methods, notifications, size limits and protocol-only stdout.
- [ ] In-flight work does not prevent reading cancellation notifications; late/duplicate cancellation is harmless.
- [ ] Self-hosted anonymous, Basic, HTTPS and custom CA cases pass real connection tests; TLS failure cannot cause a downgrade.
- [ ] AWS managed and Serverless signing use the correct service/region and verified payload hashing.
- [ ] Default-chain, named-profile, SSO-profile and explicit credential configurations behave according to the selected provider mode.
- [ ] Refreshable providers refresh credentials; expired static credentials and unusable SSO login produce actionable errors.
- [ ] Fixed AWS endpoints do not trigger node discovery or credential-bearing redirects.
- [ ] Two connections and unsaved connection tests cannot share credentials or mutable state accidentally.
- [ ] Editing connection parameters invalidates affected clients and sessions.
- [ ] Saving, editing, clearing and restarting a connection never persists credential values in ordinary connection JSON or logs.
- [ ] Missing secure storage fails visibly without plaintext persistence.

**Evidence:** subprocess results, live transport/authentication results, sanitized keychain persistence checks, race-test results and connection-isolation cases. Record unavailable live AWS cases as open validation dependencies.

## OS-001.3 — Implement explorer and document inspection

**Outcome:** indices and documents are usable through the native Tabularis explorer and grid.

**Deliverables**

- Index/alias discovery, index-pattern scoping, recursive mapping metadata and optional metadata reads.
- Parser-backed browse-query adapter, document lookup, bounded initial pages and result normalization.
- JSON inspection and narrowly scoped metadata/row actions where the host requires extensions.

**Acceptance checklist**

- [ ] Index double-click opens a populated grid through the actual host-generated query path.
- [ ] Filters, sorting and configured page sizes produce the documented native-search semantics.
- [ ] Empty indices and heterogeneous documents produce valid rectangular results.
- [ ] Nested/object fields, multi-fields, specialized types and alias targets are inspectable.
- [ ] Document results retain `_index`, `_id`, `_score` and complete `_source` where available.
- [ ] Source-field names cannot overwrite metadata columns; absent and null values are handled as specified.
- [ ] Objects/arrays and aggregation values remain inspectable JSON; large numbers are not silently rounded by the driver.
- [ ] Document lookup handles escaped IDs and required routing correctly.
- [ ] Permission-restricted metadata is shown as unavailable, not empty success or zero-valued statistics.
- [ ] Partial search failures and timeouts cannot be mistaken for complete results.
- [ ] Split-view actions cannot execute on another connection.

**Evidence:** real OpenSearch fixture results, mapping/normalization tests, host interaction recordings or screenshots, and restricted-permission cases.

## OS-001.4 — Implement query modes and safety enforcement

**Outcome:** SQL, PPL, DSL and allowlisted REST execution preserve supported native capabilities while preventing mutations.

**Deliverables**

- Native SQL/PPL adapters and schema-aware result normalization.
- DSL execution with structured search, aggregation and compatible vector-search output.
- A finite REST operation registry covering method, path, parameters, body validation, deployment support and resource effects.
- Shared validation across direct, REST-embedded and any batch execution paths.

**Acceptance checklist**

- [ ] SQL selection/filter/sort/group/aggregation and PPL field/filter/sort/statistics cases pass on supported real deployments.
- [ ] DSL search, aggregation and compatible vector queries retain their structured outputs.
- [ ] DSL `size:0` returns aggregation/diagnostic JSON with no document pagination session.
- [ ] Every exposed execution path validates the complete input before transport.
- [ ] Mutation statements, side-effecting PPL commands, unknown syntax, forbidden methods/endpoints and unregistered operations are rejected.
- [ ] Adversarial cases cover comments, quoted delimiters, mixed statements, encoded path traversal, absolute URLs and REST query-language bypass attempts.
- [ ] SQL/PPL supplied through REST receives the dedicated mode's safety checks.
- [ ] Mutation RPCs remain rejected even when called directly without the UI.
- [ ] Authorization failures are distinct from unsupported operations, invalid queries and transport failures.
- [ ] Backend read-only permissions are tested independently of client-side validation.

**Evidence:** parser/policy corpus results, verified outgoing-request boundaries, real query results and least-privilege cases. Destructive requests must not reach formal data sources during testing.

## OS-001.5 — Implement pagination and resource lifecycle

**Outcome:** bounded, isolated browsing supports deep traversal where the backend and host provide the required capabilities.

**Deliverables**

- Bounded offset pagination, PIT/search-after, verified language cursors and bounded page checkpoints.
- Session/connection/query ownership, TTL/eviction, cleanup and consistent expiry behavior.
- Deadline-aware retries and Go-context cancellation across transport and query operations.

**Acceptance checklist**

- [ ] Ordinary pages respect result-window, row, byte and time limits without retrieving an entire index.
- [ ] PIT traversal beyond the ordinary result window produces no duplicate or missing documents in a fixed snapshot.
- [ ] Deterministic sort values are preserved between continuation requests.
- [ ] Identical queries in separate sessions/connections cannot consume one another's continuation state.
- [ ] Changing query, filter, sort, page size or connection generation invalidates affected pagination state.
- [ ] PIT-backed previous-page access uses retained checkpoints while the PIT remains valid; native-cursor replay requires independent evidence, and unavailable jumps fail explicitly without unbounded replay.
- [ ] Hit exhaustion reports no further page while retaining the PIT only until session release/replacement/idle expiry; exhausted native cursors are closed.
- [ ] Requests without stable session identity do not share mutable remote cursors.
- [ ] SQL/PPL continuation and closure are verified per backend/query form; a result cap is not treated as a cursor.
- [ ] Missing schema on continuation pages uses the owning session's schema.
- [ ] Session release, TTL, eviction, cancellation, EOF and normal shutdown close only owned resources where possible.
- [ ] Cleanup failure is visible and finite remote keep-alive bounds retention after crashes/network failure.
- [ ] PIT expiry does not silently change snapshot consistency.
- [ ] Retries and cleanup respect separate documented deadlines.

**Evidence:** deep-page identity assertions, resource-lifecycle observations, concurrency/race tests, expiration/restart cases and documented backend limitations.

## OS-001.6 — Verify deployment support and prepare release

**Outcome:** a reproducible, installable release with evidence matching every compatibility claim.

**Deliverables**

- CI for Go formatting, tests/race detection, vet/static analysis, vulnerability checks, UI typecheck/lint/tests/build and packaging validation.
- Real integration suites for pinned self-hosted 2.x/3.x, managed domains and Serverless search/time-series/vector-search collections.
- ZIPs for the agreed Linux/macOS/Windows architectures with canonical manifest, executable, UI bundles, documentation, license and notices.
- Installation/configuration guides, least-privilege IAM/data-access examples, SSO prerequisites, troubleshooting and a compatibility matrix.

**Acceptance checklist**

- [ ] Required automated checks pass against the release commit.
- [ ] The matrix records exact host/client/toolchain/backend versions, image digests, AWS region, collection type/generation, test date and operation results.
- [ ] Self-hosted 2.x and 3.x tests pass; any early-2.x limitations are explicit.
- [ ] Managed-domain and all requested Serverless collection-type claims have live evidence.
- [ ] Multi-account/region/profile isolation and credential-refresh evidence is attached or remains an explicit release blocker for those claims.
- [ ] Every supported platform package passes native installation, startup and basic host-query smoke tests.
- [ ] ZIP contents, registry platform labels, executable permissions, checksums and manifest schema validation pass.
- [ ] Tag, manifest and binary versions match `0.1.0` for the initial release.
- [ ] Repository identity, publishing authority, Apache-2.0 selection and third-party notices are resolved before publication.
- [ ] Documentation examples are tested against supported deployments or explicitly labeled unverified.
- [ ] No requested feature or deployment is silently removed to complete the ticket.

**Evidence:** CI run references, integration reports, platform smoke results, archive inventories/checksums, compatibility matrix and documentation review.

## Blocker handling

Keep each blocker open until its evidence is attached:

- **Parser:** prove safe SQL/PPL coverage and browse translation. Do not replace the parser with regular-expression checks or unvalidated pass-through.
- **Host:** prove released-host mode handling, secret storage, sessions and result rendering. Any required host modification needs a separate scope decision.
- **AWS:** obtain authorized endpoints and read-only access for live validation. Local emulation does not complete AWS acceptance.
- **Backend operation:** record unsupported, unauthorized and unknown independently. State the exact affected deployment/query form.
- **License/publishing:** confirm license and repository identity before creating a publishable release.

## Completion evidence format

### Specification traceability

| Work package | Requirement coverage | Acceptance evidence |
| --- | --- | --- |
| OS-001.1 | RPC, QUERY, SAFE, UI integration baseline | AT-04, AT-06, AT-08, AT-11 feasibility evidence; gates G1/G2 decisions |
| OS-001.2 | CONN-01–06, RPC-01–04, UI-01 | AT-01–04, AT-10; transport and secret-storage evidence |
| OS-001.3 | META-01–04, QUERY-03/06, UI-02 | AT-05/06/07/11; gate G3 data semantics |
| OS-001.4 | QUERY-01–06, SAFE-01–03 | AT-06–08; final query safety/API evidence |
| OS-001.5 | PAGE-01–05, RPC-04, operational limits | AT-02/09/10; gate G4 resource evidence |
| OS-001.6 | RELEASE-01–03, UI-03, complete matrix | AT-01–12 release evidence; gate G5 closure |

AT identifiers and G gates are defined in the specification. Feasibility evidence does not replace the final feature tests.

### Work-package report

For every completed work package, record:

1. Implementation commit and changed files.
2. Specification requirement groups and acceptance items covered.
3. Exact validation commands or CI jobs, environment versions, date and result.
4. Sanitized artifacts demonstrating host/backend behavior.
5. Remaining limitations or blockers, with the acceptance items they affect.

Do not include secrets, signed authorization headers, tokens, production documents or credential-bearing endpoints in evidence artifacts. Unit fixtures support parser and normalization assertions; real host/backend tests establish integration behavior.

## Definition of done

- [ ] All six work packages meet their exit criteria with fresh evidence.
- [ ] Specification requirements for CONN, RPC, META, QUERY, SAFE, PAGE, UI and RELEASE are traceable to implementation and tests.
- [ ] Every requested feature area is implemented or an explicit user-approved scope decision is recorded.
- [ ] Deployment/operation limitations are documented and release claims match observed results.
- [ ] Security, credential isolation, cancellation and owned-resource cleanup checks pass.
- [ ] Packages install and operate on every advertised platform.
- [ ] Documentation, license, notices, version metadata and release artifacts are ready for publication under confirmed authority.

Completion of this documentation ticket file does not mark OS-001's implementation work complete.
