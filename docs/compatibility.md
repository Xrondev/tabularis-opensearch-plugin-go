# Experimental compatibility matrix

The v0.1.1 release is experimental. A successful test applies only to the operation and environment described here; it does not establish support for every API, deployment configuration, or permission policy. Unverified entries are not test passes.

## Backend and host evidence

The following checks were recorded on 30 October 2026 using Go 1.26.10 and OpenSearch Go client v5.1.3. Self-hosted CI tests use disposable Docker containers and synthetic data. AWS checks use read-only requests against existing authorized resources; they do not create or modify data.

| Environment | Evidence | Status and limits |
| --- | --- | --- |
| Self-hosted OpenSearch 2.19.3 | Disposable backend integration and race tests in CI. | Passed for the tested fixture operations; other 2.x releases and security configurations are not established by this run. |
| Self-hosted OpenSearch 3.3.2 | Disposable backend integration and race tests in CI. | Passed for the tested fixture operations; other 3.x releases and security configurations are not established by this run. |
| Tabularis Web 0.30.0 with OpenSearch 2.19.3 | Released-host HTTP RPC integration with disposable containers in the same CI run. | Passed for the scripted RPC operations. Browser login and interactive UI were not tested by this script. PIT cleanup was observed through idle expiry; see [host-test notes](../tests/host/README.md). |
| Tabularis desktop 0.30.0 on macOS arm64 | A locally installed plugin was enabled, its connection form was inspected, and read-only AWS connection testing was performed. | Passed for those checks; this is not evidence for all desktop interactions or package builds. |
| Amazon OpenSearch Service managed domain, OpenSearch 2.17, `us-east-1` | Read-only named-profile connection, index listing, and mapping retrieval. | Passed with an authorized profile. A second profile received HTTP 403; successful authentication or listing does not imply permission for all operations. |
| Amazon OpenSearch Serverless, Classic vector-search collection, `us-east-1` | Read-only named-profile connection and index discovery, including configured index-pattern filtering. | Passed for those metadata checks. Other collection policies, API operations, and collection generations remain unverified. |
| Amazon OpenSearch Serverless search and time-series collections | No live integration evidence recorded. | Unverified. |
| AWS SSO credential refresh, multiple accounts and regions, and the complete AWS operation matrix | No live integration evidence recorded. | Unverified. |

CI validates Go tests on Linux, macOS, and Windows runners, UI checks, vulnerability scanning, and package archive checks. The recorded results precede the prepared experimental release and are not evidence for its final commit; release changes require their own validation.

## Platform artifacts

ZIP archives contain the driver, manifest, PPL parser artifact, UI bundles, documentation, and license materials. A cross-build or archive integrity check does not establish native installation or runtime support. The supported Tabularis registry labels below are separate from the additional manual-build target.

| Target | Artifact label | Evidence and remaining validation |
| --- | --- | --- |
| Linux x86-64 | `linux-x64` | Cross-build and archive checks; a native Linux packaged-executable smoke test runs in CI. Full desktop installation remains unverified. |
| Linux arm64 | `linux-arm64` | Cross-build and archive checks; native installation and runtime remain unverified. |
| macOS x86-64 | `darwin-x64` | Cross-build and archive checks; native package installation and runtime remain unverified. |
| macOS arm64 | `darwin-arm64` | Cross-build, archive checks, and local packaged-executable smoke. A locally installed plugin was exercised in Tabularis desktop 0.30.0. |
| Windows x86-64 | `win-x64` | Cross-build and archive checks; Go tests run on a Windows CI runner. Native package installation remains unverified. |
| Windows arm64 | `windows-aarch64` | Additional manual experimental cross-build. Tabularis registry recognition, native installation, and runtime remain unverified. |

## Experimental release assets

Release preparation produces platform ZIPs, a standalone `default.tabularium` manifest, and a consolidated `SHA256SUMS` file. A pushed `vX.Y.Z` tag must match the manifest version after removing the leading `v`. After all required CI jobs pass, the workflow publishes the verified assets as a GitHub prerelease. Running packaging locally does not publish a release. See the [Tabularis plugin publishing guide](https://github.com/TabularisDB/tabularis/blob/main/plugins/PLUGIN_GUIDE.md#8-publishing-your-plugin).

## Known operation limits

- Connection testing lists indices and therefore requires permission for index discovery; a profile with access to a known index may lack that permission.
- SQL and PPL use validated read-only subsets. SQL cursor responses and PPL pagination are unsupported; scripted DSL searches are rejected.
- Deep PIT paging requires one concrete, single-primary-shard index, metadata permissions, and `_id` sorting. Grid filtering and sorting require explicit verified-field assurances; see [README](../README.md#current-query-behavior).
- The explorer lists indices but has no dedicated aliases, settings, or statistics view. Supported inspection operations are available through `#!rest`.
- Requests are not automatically retried. The OpenSearch-specific top-level console remains dependent on Tabularis support for plugin-provided editor tabs.
