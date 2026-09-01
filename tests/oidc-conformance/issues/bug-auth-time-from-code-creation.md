# Title

`auth_time` in the ID token is taken from authorization code creation rather than the session's authentication time

# Type

Bug (`.github/ISSUE_TEMPLATE/bug.yml`, `Type/Bug`)

---

### Description

OIDC Core §2 defines `auth_time` as "Time when the End-User authentication occurred". The
authorization code grant instead derives it from the moment the authorization code was created,
at `backend/internal/oauth/oauth2/granthandlers/authorization_code.go:196`:

```go
AuthTime: authCode.TimeCreated.Unix(),
```

With no session those two moments coincide, so the claim looks correct. Once an existing SSO
session is reused, they diverge: the user authenticated earlier, but a new authorization code
is created for each authorization request, so `auth_time` advances every time even though no
new authentication took place.

The session subsystem already records what the claim needs. `backend/internal/flow/session/`
holds the session store, resolver, state and timeouts, `SSOCheckExecutor`
(`backend/internal/flow/executor/sso_check_executor.go`) resolves an existing session, and the
Console App authentication flow uses it at
`backend/cmd/server/bootstrap/01-default-resources.yaml:274` (`start` -> `sso_check` ->
`session`, falling back to `prompt_credentials`). The plumbing to carry the value is also
present: `AuthTime` is a field on `IDTokenBuildContext`
(`backend/internal/oauth/oauth2/tokenservice/model.go:105`) and is emitted at
`backend/internal/oauth/oauth2/tokenservice/builder.go:437`. The code grant simply does not ask
the session for it.

This makes the claim wrong in exactly the case it exists for. `max_age` handling depends on it:
a client uses `auth_time` to decide whether the authentication is recent enough, and a value
that tracks code creation always looks fresh.

Note the CIBA grant handler reads a stored `record.AuthTime`
(`backend/internal/oauth/oauth2/granthandlers/ciba.go:260`) rather than a creation timestamp,
so the two grant types disagree about what the claim means.

### Steps to Reproduce

1. Complete an authorization code flow with `scope=openid` and note the `auth_time` claim in
   the ID token.
2. Without re-authenticating, complete a second authorization for the same user and compare the
   `auth_time` claim in the new ID token.

The value differs between the two tokens, because each authorization mints a new code and
`auth_time` is read from that code's creation timestamp rather than from when the user
authenticated.

Found running the OpenID Foundation conformance suite, Basic OP plan.
`oidcc-max-age-10000` authorizes once, then authorizes again with `max_age=10000`. The existing
authentication is well inside that window, so §3.1.2.1 requires it to be reused and `auth_time`
to stay the same across both ID tokens. It fails:

```
CheckIdTokenAuthTimeClaimsSameIfPresent: The id_tokens contain different auth_time
claims, but must contain the same auth_time.
```

Two related modules, `oidcc-max-age-1` and `oidcc-prompt-login`, pass their `auth_time`
assertions (`CheckSecondIdTokenAuthTimeIsLaterIfPresent: auth_time is later in the second
id_token`) and then stop on an unrelated harness limitation, so they neither confirm nor
contradict this.

#### Expected

`auth_time` should carry the time the user actually authenticated, taken from the session that
authorization resolved, so that it stays stable while a session is reused and only advances on
a genuine re-authentication.

Fixing this on its own does not make `oidcc-max-age-10000` pass, because the authorization
endpoint does not consult an existing session for OAuth clients yet; that part is tracked as a
separate improvement. This issue is the claim being derived from the wrong source, which is
worth correcting independently.

### Version

v1.0.0 (`oidc-conformnace-suite` branch, distribution `thunderid-1.0.0-macos-arm64`)

### Environment Details (with versions)

macOS, SQLite (default deployment), server on `https://thunderid.conformance.test:8090`.
