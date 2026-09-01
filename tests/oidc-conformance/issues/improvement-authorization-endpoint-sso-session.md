# Title

Consult the existing SSO session at the authorization endpoint so `prompt=none`, `id_token_hint` and `max_age` work

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

The authorization endpoint does not consider an existing authenticated session, so three OIDC
Core §3.1.2.1 parameters cannot behave as specified.

`ValidatePromptParameter` returns `login_required` for every `prompt=none` request without
looking anything up, at
`backend/internal/oauth/oauth2/authz/requestvalidator/validator.go:123`:

```go
// The server does not support server-side sessions as of now.
return constants.ErrorLoginRequired,
    "User authentication is required"
```

That comment is no longer accurate. The session subsystem exists and is in use:

- `backend/internal/flow/session/` provides the store, resolver, cookie transport, state and
  idle/absolute/refresh timeouts.
- `SSOCheckExecutor` (`backend/internal/flow/executor/sso_check_executor.go`) resolves an
  existing session and branches on whether one was found.
- The Console App authentication flow already uses it at
  `backend/cmd/server/bootstrap/01-default-resources.yaml:274`: `start` -> `sso_check` ->
  `session`, with `prompt_credentials` as the failure path.
- `flowexec` wires the SSO cookie and its lifetime at
  `backend/internal/flow/flowexec/init.go`.

So this is not missing infrastructure. The authorization endpoint does not route through it for
OAuth clients, and the prompt validator predates it.

Three consequences, all confirmed by a conformance run against the Basic OP plan:

| Module | Failure |
|---|---|
| `oidcc-prompt-none-logged-in` | `CheckIfAuthorizationEndpointError: The authorization was expected to succeed, but the server returned an error from the authorization endpoint` |
| `oidcc-id-token-hint` | Same error. `prompt=none` returns before `id_token_hint` is read, so the hint is never used to identify the authenticated user. |
| `oidcc-max-age-10000` | `CheckIdTokenAuthTimeClaimsSameIfPresent: The id_tokens contain different auth_time claims, but must contain the same auth_time.` |

`oidcc-prompt-none-not-logged-in` passes, because `login_required` is the correct answer when
nobody is signed in. It is the only answer the server gives, so it is right by coincidence
rather than by logic.

Two further modules, `oidcc-prompt-login` and `oidcc-max-age-1`, exercise session reuse and
re-authentication. Both currently stop on a separate harness limitation, so their behaviour
here is unmeasured and may also change.

### Suggested Improvement

Route the OAuth authorization endpoint through the same session resolution the flow engine
already performs, and make the three parameters consult its result:

1. **`prompt=none`** should return a successful authorization when a valid session exists, and
   `login_required` only when it does not. This replaces the unconditional return in
   `ValidatePromptParameter`, which should take session state as an input rather than deciding
   from the parameter alone.
2. **`id_token_hint`** should be validated and used to identify the already-authenticated user,
   rather than being unreachable behind the `prompt=none` short circuit.
3. **`max_age`** should compare the session's authentication time against the requested window
   and re-authenticate only when it has been exceeded. This depends on `auth_time` being
   derived from the session rather than from authorization code creation, which is a separate
   bug worth fixing first.

Worth checking as part of this: the SSO flow above is attached to the Console App flow, and it
is not clear which flow a dynamically registered client receives. If the default flow for DCR
clients omits `sso_check`, part of this is a flow configuration question rather than a code
change, which would make it smaller than it looks.

Beyond the conformance modules, this is the foundation for RP-initiated logout and the Session,
Front-Channel and Back-Channel OP profiles, none of which can work without the authorization
endpoint knowing about sessions.
