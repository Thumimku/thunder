# OIDC Conformance Tests

Runs ThunderID against the [OpenID Foundation conformance suite](https://gitlab.com/openid/conformance-suite)
to check its OpenID Connect implementation against the specification.

The CI entry point is [`.github/workflows/oidc-conformance-test.yml`](../../.github/workflows/oidc-conformance-test.yml),
which runs nightly at 19:30 UTC and on demand from the Actions tab.

## What runs

The default plan is `oidcc-basic-certification-test-plan` (the **Basic OP** profile, 38 modules).
That is the right plan for ThunderID: it exercises `response_type=code` with the `query`
response mode only, which is exactly what the server advertises. The implicit and hybrid plans
do not apply — ThunderID supports neither.

Clients are registered through Dynamic Client Registration, which is enabled by default
(`oauth.dcr.enabled` in `default.json`), so no client has to be provisioned in advance.

### The other OP profiles

Basic OP is one of three provider profiles that apply to ThunderID. `run_tests.py --plan` selects
between them.

| Plan | Modules | State |
| --- | --- | --- |
| `oidcc-basic-certification-test-plan` | 38 | The default. 29 counted as passing; see the results below. |
| `oidcc-config-certification-test-plan` | 1 | Runs. Passes with one warning. |
| `oidcc-dynamic-certification-test-plan` | 23 | Blocked: no DCRM. See the results below. |

**Config OP** is a single module, `oidcc-discovery-endpoint-verification`, which only reads the
discovery document. It needs no browser, so run it with `--no-browser`.

Plans differ in which variants they let the caller choose. Basic OP asks for `server_metadata`
and `client_registration`; Config OP declares none and pins them per module, so sending one is
a 400:

```
HTTP 400 Variant 'server_metadata' has been set by user, but test plan already sets
this variant for module 'oidcc-discovery-endpoint-verification'
```

`run_tests.py` therefore reads the plan's declared variants from `GET /api/plan/available` and
sends only the keys that plan accepts, rather than assuming Basic's set applies everywhere.

**Dynamic OP** shares only 4 of its 23 modules with Basic. The other 19 cover areas Basic never
reaches: redirect URI validation (5 modules), registration metadata such as `logo_uri`,
`sector_identifier_uri` and `jwks_uri` (6), RS256-signed ID tokens and UserInfo responses plus
signing-key rotation (4), `request_uri` handling (2), and `private_key_jwt` client authentication
(1). It also needs a `response_type` variant, which is `code` for ThunderID.

It cannot run today. The results section below records what blocks it and what a run would need.

## Scripts

| Script | Role |
| --- | --- |
| `configure_server.py` | Unpacks the distribution, patches `deployment.yaml`, issues a certificate, runs setup and starts the server. |
| `setup_test_user.py` | Obtains an admin token and creates the user the suite logs in as. |
| `patch_compose.py` | Adds the `extra_hosts` mapping and mounts the truststore into the suite's compose file. |
| `start_conformance_suite.py` | Brings the suite up under Docker Compose and waits for it to serve. |
| `run_tests.py` | Creates the plan, runs every module, drives front-channel steps, and reports results. |
| `browser_driver.py` | Visits the URLs a module publishes, signing in with Chromium via Playwright. |
| `plan-config.json` | Plan configuration template. |

## Why `deployment.yaml` gets patched

The suite runs in Docker and must reach the server on a hostname that matches both its TLS
certificate and its OIDC issuer. There is no environment variable override, and
`start.sh --port` only changes a pre-flight check, not the bound port, so the harness rewrites
`deployment.yaml` before the first start. Three things are set there:

- **`server.hostname` is set to `0.0.0.0`, not the conformance hostname.** ThunderID binds to
  whatever `hostname` says. A conformance hostname normally resolves to `127.0.0.1`, so binding
  to it leaves the server on loopback where the suite's containers cannot reach it —
  connections are refused at the gateway address.
- **`server.public_url` carries the public identity instead.** ThunderID prefers it over the
  derived host/port when building the issuer, so the discovery document and the `iss` claim
  stay correct while the socket listens everywhere.
- **`oauth.dcr.insecure` is turned on.** The suite registers its relying parties anonymously.
  ThunderID ships with `insecure: false`, which demands an authenticated caller holding the
  right permission and answers `unauthorized_client` otherwise — which fails every module in
  the plan before it starts. This is a test-server-only setting and must never be used in a
  real deployment.

`setup.sh` issues a `CN=localhost` certificate, so `configure_server.py` re-issues one with the
conformance hostname in its SAN list afterwards.

## How the login step is driven

The suite drives front-channel interaction with HtmlUnit, which does not execute ES modules.
ThunderID's login page (`/gate/signin`) is a client-side React app: the served HTML is an empty
`<div id="root">` plus `<script type="module" src="/gate/assets/index-*.js">`. HtmlUnit never
requests that bundle, so no form is ever rendered and the automation times out:

```
WebRunner | Response 200 OK GET .../gate/signin?...
WebRunner | Response 200 OK GET .../gate/assets/index-cU0xs27d.css
WebRunner | Timed out waiting: ["wait","id","username",15]
```

The form is not merely rendered by JavaScript, it is *described by the server*: the bundle calls
`/flow/execute`, receives the set of fields the configured authentication flow requires, and
renders them at runtime. There is no static form anywhere in the frontend to fall back to.

So `plan-config.json` carries no `browser` block. With no automation entry matching a URL, the
suite's `BrowserControl.goToUrl()` leaves it in the module's `urls` list for someone else to
visit. `run_tests.py` polls `GET /api/runner/browser/{id}` between status checks, opens each new
URL in Chromium through `browser_driver.py`, signs in, waits for the redirect back to the suite,
and reports the URL via `POST /api/runner/browser/{id}/visit`. The suite scores the module
exactly as it would have.

Two details this depends on:

- **TLS.** Both the server and the suite serve certificates that are not in the system trust
  store, so the browser context runs with `ignore_https_errors`. This is the Playwright
  equivalent of the truststore `patch_compose.py` mounts for HtmlUnit, and it talks only to the
  two hosts under test.
- **A fresh context per visit.** Sessions must not leak between modules, or a module that
  assumes a logged-out user would see one already signed in.

Pass `--no-browser` to skip this and leave front-channel URLs unvisited, or `--headed` to watch
a login in a real window while debugging.

Alternatives considered and rejected:

- **Serve a no-JS fallback form.** What most certified providers do, but it does not apply here:
  with no static form in the frontend, a fallback means rendering `/flow/execute`'s component
  descriptors to HTML server-side, accepting form-encoded posts, and carrying multi-step flow
  state across requests — a second implementation of the flow engine that would drift from the
  real one.
- **A different conformance suite.** The OpenID Foundation suite is the only implementation
  accepted for certification.

Note that a certification *submission* is done interactively regardless, since several modules
require screenshots and manual review. The value of this path is a trustworthy nightly signal.

## Results

Basic OP and Config OP were last run on 2026-08-19 against the distribution built from this
branch. Dynamic OP has not been run; its section records what blocks it.

| Profile | Modules | Passing | Failing | State |
| --- | --- | --- | --- | --- |
| Basic OP | 38 | 29 | 9 | Run. |
| Config OP | 1 | 1 | 0 | Run. |
| Dynamic OP | 23 | — | — | Blocked before the first module. |

Two of Basic OP's nine failures are a harness limitation rather than a server gap, so the real
count there is seven. Each section below records the request a module sends, what the
specification requires in response, and what ThunderID does instead.

### Config OP: passes with one warning

`oidcc-discovery-endpoint-verification` validates the discovery document against RFC 8414 and
OIDC Discovery (`RFC8414-2`, `OIDCD-3`). Every endpoint, the issuer, the JWKS URI and the
registration and revocation endpoints validate. One warning:

```
CheckForUnexpectedParametersInServerMetadata
unknown_properties: [{"property": "authorization_grant_profiles_supported"}]
```

`authorization_grant_profiles_supported` appears in the metadata but is not defined by RFC 8414,
OIDC Discovery, or any specification the suite recognises. Either it is a deliberate extension,
in which case adding it to `server.allow_unexpected_metadata_fields` in the plan config
suppresses the warning, or it is a stray field worth removing, since unrecognised top-level
metadata can confuse strict clients. Worth tracing why it is advertised before choosing.

This makes Config OP the profile closest to a submittable state today.

### Gap: no server-side SSO session

Three modules fail on one missing capability. `ValidatePromptParameter` in
`backend/internal/oauth/oauth2/authz/requestvalidator/validator.go` returns `login_required`
for every `prompt=none` request without consulting any session state, and nothing else in the
authorization path tracks an existing authentication either.

- **`oidcc-prompt-none-logged-in`** — authorizes normally, then repeats the request with
  `prompt=none`. OIDC Core §3.1.2.1 requires the second request to succeed without UI, because
  the user is already authenticated. ThunderID answers `login_required`, and the suite reports
  *"The authorization was expected to succeed, but the server returned an error from the
  authorization endpoint"*. Note that `oidcc-prompt-none-not-logged-in` passes: `login_required`
  is the right answer when nobody is signed in, and it is the only answer ThunderID gives.
- **`oidcc-id-token-hint`** — repeats the authorization with the first ID token in
  `id_token_hint`, which §3.1.2.1 expects the server to use to identify the authenticated user.
  The `prompt=none` branch returns before `id_token_hint` is read, so this fails identically.
- **`oidcc-max-age-10000`** — repeats the authorization with `max_age=10000`. The existing
  authentication is well inside that window, so §3.1.2.1 requires it to be reused and the
  `auth_time` claim to stay put. With no session the user re-authenticates and the claim moves:
  *"The id_tokens contain different auth_time claims, but must contain the same auth_time."*

Closing this needs a session store, a session cookie issued at the authorization endpoint, and
`prompt`, `id_token_hint` and `max_age` all consulting it. `oidcc-prompt-login` and
`oidcc-max-age-1` also exercise session reuse and currently stop on the harness limitation
below, so expect their behaviour to change too.

### Gap: no JAR (RFC 9101)

Two modules pass authorization parameters inside a JWT. ThunderID implements neither form:
`request_uri` exists only as a PAR handle, and `request` is not read at all.

- **`oidcc-unsigned-request-object-supported-correctly-or-rejected-as-unsupported`** — sends the
  parameters in an unsigned JWT in `request`. The module accepts either behaviour its name
  describes: honour the object, or reject it with `request_not_supported`. ThunderID does
  neither. It ignores the parameter and proceeds on the query string alone, so the `state` and
  `nonce` carried inside the object are lost: *"State was passed in request, but is missing from
  response"* and *"Nonce values mismatch"*. Silently ignoring is the one outcome the
  specification does not allow; returning `request_not_supported` would pass this module without
  implementing JAR at all.
- **`oidcc-request-uri-unsigned-supported-correctly-or-rejected-as-unsupported`** — the same by
  reference, via `request_uri`. The module timed out after the redirect to the authorization
  endpoint. `request_uri_not_supported` is the equivalent cheap fix.

### Warnings

Warnings do not fail the run, matching how the suite grades certification, but each one is a
real deviation.

- **`VerifyScopesReturnedInUserInfoClaims`** (six modules: `oidcc-scope-profile`,
  `-email`, `-address`, `-phone`, `-all`, and `oidcc-alternate-happy-flow`) — authorizes with
  the standard scopes and checks UserInfo carries the claims each one implies, per §5.4. The
  scope-to-claim table in `backend/internal/oauth/oauth2/constants/constants.go` is correct
  (`phone` maps to `phone_number`), but the built-in Person schema stores **`mobile_number`** and
  has **no `address` attribute at all**, so the mapping asks for claims the schema cannot supply.
  This is the same naming mismatch that makes `setup_test_user.py` send `mobile_number`. Deciding
  it affects every OIDC client, not just this suite.
- **`oidcc-claims-essential`** — sends `claims={"userinfo":{"name":{"essential":true}}}` and
  expects `name` in UserInfo. The suite reports *"name not found in userinfo"* even though the
  test user has the attribute, so the `claims` request parameter is not driving the UserInfo
  response. Worth weighing against discovery advertising `claims_parameter_supported: true`.
- **`oidcc-userinfo-post-body`** — posts to the UserInfo endpoint with the token in a
  form-encoded body and gets a non-2xx answer. This one is expected and needs no change: RFC
  6750 §2.2 says resource servers *MAY* support that method and that it *SHOULD NOT* be used
  except where browsers cannot set the `Authorization` header. Supporting the header form only
  is conformant. `oidcc-userinfo-get` and `oidcc-userinfo-post-header` both pass.
- **`oidcc-ensure-request-with-acr-values-succeeds`** — requests `acr_values` and expects an
  `acr` claim back. The specification says SHOULD, hence a warning rather than a failure.

### Harness limitation: modules that wait for a screenshot

Four modules complete every assertion and then wait for a human to upload a screenshot or
confirm a rendered page. `browser_driver.py` does not satisfy those placeholders, so they sit in
`WAITING` until the module timeout retires them.

- **`oidcc-prompt-login`** and **`oidcc-max-age-1`** record *zero* failures. Both reach
  `CheckSecondIdTokenAuthTimeIsLaterIfPresent: auth_time is later in the second id_token` and
  pass it. They are only reported as failures because the harness cannot finish them.
- **`oidcc-ensure-registered-redirect-uri`** and **`oidcc-ensure-request-object-with-redirect-uri`**
  end on `[REVIEW] ExpectRedirectUriErrorPage: Show redirect URI error page`, waiting for
  confirmation that an error page rendered.

Until the driver handles these, read those four as "not measured" rather than as failures. This
is also a reminder that a certification submission is interactive regardless, since the same
screenshots need manual sign-off.

### Dynamic OP: not run, two hard blockers

This profile has not been run. The findings below come from reading the plan's requirements and
probing the DCR endpoint directly, not from a conformance run, so treat them as prerequisites
rather than results. Two of them stop the profile regardless of how the harness is configured.

**No DCRM (RFC 7592).** Registration succeeds, but the `201` response omits both
`registration_access_token` and `registration_client_uri`:

```
POST /oauth2/dcr/register  ->  201
  client_id: fpIquJvJb4kQycQ_k-KJnQ
  registration_access_token: ABSENT
  registration_client_uri: ABSENT
```

RFC 7591 §3.2.1 returns those two fields when client management is supported, and RFC 7592 §2
defines the `GET`/`PUT`/`DELETE` operations they authorise. Without them a client registered
anonymously can never be read back, updated or deleted. Basic OP only needs this for cleanup,
which is the `UnregisterDynamicallyRegisteredClient: Couldn't find registration_access_token`
line in every one of its modules. Dynamic OP needs it for test content:
`oidcc-server-rotate-keys` and `oidcc-refresh-token-rp-key-rotation` rotate a registered
client's keys, which is a `PUT` to `registration_client_uri`.

This is the smaller of the two gaps to close. It needs a token issued at registration, stored
against the client, and a per-client management URI that authorises against it. No session or
signing infrastructure is involved. Worth deciding alongside `oauth.dcr.insecure`: a
registration access token is how a registrant manages only what it created, which is a narrower
grant than the current all-or-nothing switch.

**No `sector_identifier_uri`.** The field does not appear anywhere in the backend. DCR accepts
it and silently drops it, so a client registering one gets a `201` and no effect. Two modules
test it: `oidcc-registration-sector-uri` expects it honoured, and `oidcc-registration-sector-bad`
expects a registration to be *rejected* when the URI's contents do not list the client's own
redirect URI, which means partial support cannot pass.

Note that this is really two features. OIDC Core §8.1 uses the sector identifier to derive a
pairwise `sub`, so that several clients in one sector share an identifier while staying
unlinkable to clients outside it. ThunderID advertises `subject_types_supported: ["public"]` and
does not implement pairwise subjects at all, so sector identifiers have nothing to attach to
yet. Basic OP does not require pairwise; only Dynamic OP tests it. Silently accepting the field
is worth fixing either way.

**Two more gaps, smaller in scope.** `id_token_signed_response_alg` is dropped at registration,
so a client cannot select its own signing algorithm even though discovery advertises `RS256` and
`ES256` server-wide; `oidcc-idtoken-rs256` and `oidcc-userinfo-rs256` need that. And the four
request-object modules (`oidcc-request-uri-unsigned`, `-signed-rs256`,
`oidcc-ensure-request-object-with-redirect-uri`) need the same JAR support Basic OP already
found missing, which discovery confirms by omitting
`request_object_signing_alg_values_supported`, `request_parameter_supported` and
`request_uri_parameter_supported`.

**What already works.** More than expected. `private_key_jwt` is in
`token_endpoint_auth_methods_supported`, and DCR round-trips `logo_uri`, `tos_uri`, `policy_uri`,
`jwks_uri` and `jwks`. So `oidcc-registration-logo-uri`, `-policy-uri`, `-tos-uri`,
`-jwks-uri` and `oidcc-ensure-client-assertion-with-iss-aud-succeeds` have a plausible chance
once the plan can run at all.

**Harness prerequisite.** The plan's `configurationFields` require `client.jwks` and
`client2.jwks`, which `plan-config.json` does not carry, plus a `response_type` variant (`code`
for ThunderID). That means a second config file holding two RSA keypairs in JWKS form.

Running it before DCRM lands is not worth much: the missing registration access token would
fail or stall a large share of the 23 modules for one reason, which buries everything else the
run would tell you.

## Running locally

Requires Docker, JDK 21, Maven, Go, pnpm and Python 3. The conformance suite compiles with
`release 21`, so an older JDK fails its Maven build with `release version 21 not supported`.

Two macOS-specific gotchas, neither of which affects the Linux CI runner:

- **Use a Python linked against OpenSSL, not LibreSSL.** ThunderID requires TLS 1.3, and the
  system Python that ships with macOS is built on LibreSSL 2.8.3, which cannot negotiate it at
  all — every request fails the handshake and the server looks like it never started.
  `configure_server.py` checks this up front and says so. Homebrew's `python@3.12` works.
- **Docker on macOS reaches the host at `host.docker.internal`, not `host-gateway`.** Pass the
  address that resolves to (`192.168.5.2` on Rancher Desktop, discoverable with
  `docker run --rm alpine getent hosts host.docker.internal`) to `patch_compose.py --ip`.
  Docker Desktop also only bind-mounts paths it is configured to share, which normally means
  somewhere under `/Users` — a checkout under `/tmp` silently mounts as an empty directory.

```bash
pip3 install httpx pyyaml playwright
python3 -m playwright install chromium

# Build a distribution.
make clean && make build OS=$(go env GOOS) ARCH=$(go env GOARCH)

# Start the server. Use localhost locally; CI uses a dedicated hostname.
python3 tests/oidc-conformance/configure_server.py \
  "$(find target/dist -name 'thunderid-*.zip' | head -1)" /tmp/thunderid-dist \
  --hostname localhost --port 8090

python3 tests/oidc-conformance/setup_test_user.py --base-url https://localhost:8090

# Build and start the conformance suite.
git clone --depth 1 https://gitlab.com/openid/conformance-suite.git
(cd conformance-suite && mvn -B clean package -DskipTests)
python3 tests/oidc-conformance/start_conformance_suite.py conformance-suite/docker-compose-dev.yml

# Run the plan.
python3 tests/oidc-conformance/run_tests.py \
  --base-url https://localhost:8090 \
  --config tests/oidc-conformance/plan-config.json

# Or the single-module Config OP profile, which needs no browser.
python3 tests/oidc-conformance/run_tests.py \
  --base-url https://localhost:8090 \
  --config tests/oidc-conformance/plan-config.json \
  --plan oidcc-config-certification-test-plan \
  --no-browser
```

Results are printed as a table, written to `conformance-results.json`, and the suite's own UI at
`https://localhost:8443` has the per-module logs.

Budget at least an hour of wall clock for a full Basic OP run, and do not wrap it in a short
`timeout`. Modules that stall are only given up on after `MODULE_TIMEOUT_SECONDS` (300s each), so
a handful of them dominates the total. Config OP takes seconds.

Progress is printed as each module finishes rather than buffered to the end, and an interrupted
run still writes its results file, naming the modules that never ran and exiting non-zero. The
suite also keeps its own copy either way: `GET /api/plan` lists the plans, and
`GET /api/info/{module_id}` gives the status and result of every module instance a plan
records, which is where the per-module failure and warning detail above came from.

## Notes

- TLS is handled differently on the suite's two paths, and both matter. Its backend HTTP client
  (discovery, token, UserInfo) installs a trust-all manager and a no-op hostname verifier, so a
  self-signed certificate is fine there. Its browser automation runs on HtmlUnit, which never
  calls `setUseInsecureSSL`, so the login page *is* verified normally. That is why
  `patch_compose.py` mounts a truststore containing the server certificate and points the JVM
  at it — without that, every module fails at the authorization step.
- The suite advertises itself as `https://localhost.emobix.co.uk:8443`, a real hostname that
  resolves to `127.0.0.1`. The scripts dial that name rather than `localhost` so its own
  certificate matches.
- The suite's API is unauthenticated in dev mode (`--fintechlabs.devmode=true` in
  `docker-compose-dev.yml`), so the runner needs no token.
- Each plan module carries a resolved variant that `run_tests.py` passes back when starting it.
  The Basic plan pins client authentication per module — most use `client_secret_basic` and one
  uses `client_secret_post` — so dropping that variant would fail that module spuriously.
