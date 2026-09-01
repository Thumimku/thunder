# Running the conformance suite locally (macOS)

The CI workflow in [`.github/workflows/oidc-conformance-test.yml`](../../.github/workflows/oidc-conformance-test.yml)
is written for an Ubuntu runner. Four of its assumptions do not hold on a Mac, and each one
fails in a way that points somewhere other than the cause. This runbook is the working
sequence plus those four traps.

Read [`README.md`](README.md) first for what the suite tests and why the harness drives the
browser itself. This file is only about getting a run started on a local machine.

## Prerequisites

| Requirement | Check |
| --- | --- |
| Docker running | `docker info` |
| Suite checked out | a clone of `https://gitlab.com/openid/conformance-suite` |
| Suite jar built | `target/fapi-test-suite.jar` exists in that clone |
| `/etc/hosts` entry | `127.0.0.1 thunderid.conformance.test` |
| Python env | `httpx`, `playwright` + `playwright install chromium` |
| `keytool` | ships with any JDK |

If the suite jar is missing, build it in the suite clone with `mvn -B clean package -DskipTests`.

Paths below assume the layout this was written against — a ThunderID worktree holding the
harness, and the suite cloned as a sibling:

```
WorkSpace/
  oidc-conformance-suite/     # ThunderID worktree (harness + backend)
  conformance-suite/          # the OpenID Foundation suite
  conformance-venv/           # python env
```

Set these once per shell:

```bash
export TID=/path/to/WorkSpace/oidc-conformance-suite
export SUITE=/path/to/WorkSpace/conformance-suite
export PY=/path/to/WorkSpace/conformance-venv/bin/python
export WORK=$(mktemp -d)          # dist, logs, cert, truststore
export HOST=thunderid.conformance.test
export PORT=8090
```

## The four macOS traps

### 1. Do not use `docker-compose-dev-mac.yml`

It bind-mounts `./mongo/data` from the host. The mongo image runs as uid 999 and cannot chown
a directory owned by your user:

```
mongodb-1 | chown: changing ownership of '/data/db': Permission denied
```

Mongo exits, the Java server then dies with `UnknownHostException: mongodb`, and **only nginx
survives** — so the visible symptom is nginx 502s, not a mongo error. Use
`docker-compose-dev.yml`, which declares a named volume (`mongo-data:`) and sidesteps host
permissions. It is also the file CI patches, so it is the better-tested path.

### 2. `patch_compose.py` reverts trap 1

`patch_compose.py` rewrites `docker-compose-dev.yml`. If that file has been `git checkout`-ed
or freshly cloned, it carries the bind mount again. Apply the named volume **after** patching,
every time:

```bash
python3 - <<'EOF'
import io, os
p = os.path.join(os.environ["SUITE"], "docker-compose-dev.yml")
s = io.open(p, encoding="utf-8").read()
s = s.replace("    - ./mongo/data:/data/db", "    - mongo-data:/data/db")
if "\nvolumes:\n" not in s:
    s = s.rstrip("\n") + "\nvolumes:\n  mongo-data:\n"
io.open(p, "w", encoding="utf-8").write(s)
EOF
```

### 3. The host IP is not on `eth0`

CI reads the runner IP with `ip -o -4 addr list eth0`. On Docker Desktop the container-to-host
gateway is what `host.docker.internal` resolves to — commonly `192.168.5.2`, but resolve it
rather than hardcoding:

```bash
export HOSTIP=$(docker run --rm alpine sh -c \
  "getent hosts host.docker.internal | awk '{print \$1}'" | head -1)
```

Your `en0` address (`ipconfig getifaddr en0`) is **not** the right value.

### 4. nginx caches the upstream IP at startup

nginx resolves the `server` container once, when it starts. Restart or recreate the server
container and nginx keeps 502-ing at the stale address — which reads as "the suite failed to
start" even though the Java app booted fine. Check the server log for
`Started Application in N seconds`; if it is there, recreate nginx:

```bash
docker compose -f "$SUITE/docker-compose-dev.yml" up -d --force-recreate nginx
```

Related: `start_conformance_suite.py` allows 300s, but the poll can time out while the Java app
is still coming up. A timeout is not proof of failure — always check the server log before
re-running anything.

## The sequence

### 1. Build the distribution

From the ThunderID worktree. Any change to `backend/cmd/server/bootstrap/01-default-resources.yaml`
(flow definitions) or backend code needs a rebuild — the server reads the bootstrap file from
the unpacked distribution, not from the repo.

```bash
cd "$TID"
make build OS=$(go env GOOS) ARCH=$(go env GOARCH)
export DIST=$(find target/dist -name "thunderid-*.zip" | head -1)
```

Confirm a flow edit actually reached the zip before chasing a failing module:

```bash
unzip -p "$DIST" '*/bootstrap/01-default-resources.yaml' | grep -c SSOCheckExecutor
```

### 2. Configure and start the server

```bash
$PY tests/oidc-conformance/configure_server.py "$DIST" "$WORK/dist" \
  --hostname "$HOST" --port "$PORT" \
  --log-file "$WORK/thunderid-server.log" \
  --cert-out "$WORK/server-cert.pem"
```

This unpacks the zip, patches `deployment.yaml` (binds `0.0.0.0`, sets `public_url`, enables
`oauth.dcr.insecure`), runs `setup.sh`, re-issues the certificate with `$HOST` in its SAN list,
and starts the server. Admin credentials default to `admin`/`admin`.

It ends with `>>> ThunderID is ready for conformance testing.` and a discovery summary.

### 3. Create the test user

```bash
$PY tests/oidc-conformance/setup_test_user.py \
  --base-url "https://$HOST:$PORT" \
  --username conformance-user --password 'Conformance@123'
```

### 4. Build the truststore

From the certificate step 2 just issued — **not** any `.jks` left over from a previous run,
which holds a stale certificate and fails every browser-driven module at the TLS handshake.

```bash
rm -f "$WORK/thunderid-truststore.jks"
keytool -importcert -noprompt -alias thunderid \
  -file "$WORK/server-cert.pem" \
  -keystore "$WORK/thunderid-truststore.jks" -storepass changeit
```

### 5. Patch compose, then re-apply the named volume

```bash
$PY tests/oidc-conformance/patch_compose.py "$SUITE/docker-compose-dev.yml" \
  --host "$HOST" --ip "$HOSTIP" \
  --truststore "$WORK/thunderid-truststore.jks"
```

Then run the snippet from **trap 2** — patching restores the bind mount.

### 6. Start the suite

```bash
$PY tests/oidc-conformance/start_conformance_suite.py "$SUITE/docker-compose-dev.yml"
```

### 7. Verify before running 38 modules

A container that cannot reach the server fails every module at the same first step, which reads
as 38 unrelated conformance failures. Two checks, ten seconds:

```bash
# the suite itself is serving
curl -sk -o /dev/null -w '%{http_code}\n' \
  https://localhost.emobix.co.uk:8443/api/runner/available     # expect 200

# the suite's container can reach ThunderID
docker compose -f "$SUITE/docker-compose-dev.yml" exec -T server \
  curl -sk --max-time 15 -o /dev/null -w '%{http_code}\n' \
  "https://$HOST:$PORT/.well-known/openid-configuration"       # expect 200
```

A 502 on the first is trap 4. A failure on the second is trap 3.

### 8. Run the plan

```bash
$PY tests/oidc-conformance/run_tests.py \
  --base-url "https://$HOST:$PORT" \
  --config tests/oidc-conformance/plan-config.json \
  --plan oidcc-basic-certification-test-plan \
  --username conformance-user --password 'Conformance@123' \
  --results-file "$WORK/conformance-results.json"
```

Basic OP is 38 browser-driven modules and takes a while. The log prints a plan-detail URL —
open it to watch module results land live. Useful flags:

- `--headed` — watch a login in a real window while debugging one.
- `--no-browser` — skip front-channel entirely. Correct for
  `oidcc-config-certification-test-plan` (one module, no login); every login-dependent module
  in Basic OP will time out.

`run_tests.py` runs the **whole plan**; there is no module filter. To check specific modules,
run the plan and read their lines out of the results file.

## Iterating on a change

After the first full setup, a backend or flow change needs only:

```bash
cd "$TID" && make build OS=$(go env GOOS) ARCH=$(go env GOARCH)
# stop the old server (see below), then repeat steps 2 and 8
```

The suite containers can stay up across iterations. Two things to keep in mind:

- Re-running `configure_server.py` issues a **new certificate**, so redo steps 4 and 5 and
  recreate nginx (trap 4) — otherwise browser-driven modules fail on TLS.
- A flow-definition change bumps the flow's `ActiveVersion`, which invalidates existing SSO
  sessions by design (`Service.Resolve` drops sessions whose `FlowVersion` differs). Expect the
  first run after such a change to re-authenticate.

Stop the server between iterations:

```bash
lsof -ti:$PORT | xargs kill        # or "$WORK/dist"/*/stop.sh if present
```

Tear down the suite:

```bash
docker compose -f "$SUITE/docker-compose-dev.yml" down
```

## Use absolute paths

Every harness script is invoked here with a path relative to the ThunderID worktree. If your
shell's working directory is anywhere else, the failure is a bare `can't open file` with no
mention of the directory — easy to misread as a broken checkout:

```
python: can't open file '/…/WorkSpace/tests/oidc-conformance/run_tests.py': [Errno 2] …
```

`run_tests.py` also exits 0 in that case, so a backgrounded run appears to have succeeded while
having done nothing. Prefer `"$TID/tests/oidc-conformance/<script>.py"` over the relative form,
and check the first lines of the log for `Modules to run: 38` before assuming a run is underway.

## When a module fails

1. Read the plan-detail URL in the run log — the suite shows the exact assertion that failed.
2. Check `$WORK/thunderid-server.log` for the matching request.
3. Confirm your change is in the running distribution (step 1) before assuming it did not work.
4. Check `README.md`'s results section — several modules have known causes recorded there, and
   two Basic OP failures are harness limitations rather than server gaps.
