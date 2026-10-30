# Released host integration

Run `bash scripts/ci-tabularis-host.sh` with Docker, Go, curl, jq and the built UI assets available. The test creates disposable containers and data, binds published ports to loopback, and uses synthetic credentials. It exercises the released Tabularis 0.30.0 HTTP RPC interface and does not test the browser UI.

The 0.30.0 login page sends `Referrer-Policy: no-referrer`; Chrome can consequently send `Origin: null` on form submission and receive a 403. This test sends an explicit same-origin header for HTTP API authentication and does not establish browser login compatibility.

The released host accepts `release_query_session`, but does not forward release for this nontransactional session. The test checks that the PIT is eventually cleaned up through idle expiry; it does not assert immediate tab-close cleanup. Set `TABULARIS_HOST_TEST_PORT` to select another loopback port when 18388 is occupied.
