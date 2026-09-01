# Title

Derive a dynamically registered client's allowed user attributes from its granted scopes

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

Claim release is gated on a per-application allowlist, and dynamic client registration never
populates it. A dynamically registered client therefore receives a UserInfo response containing
only `sub`, whatever scopes it was granted and whatever it requests through the `claims`
parameter.

The claims machinery itself is complete. UserInfo extracts the `claims` request from the access
token and passes its `userinfo` map into claim building
(`backend/internal/oauth/oauth2/userinfo/service.go:414` and `:447`), and `BuildClaims`
implements the OIDC Core §5.5 precedence rules, with explicit claims overriding scope claims
(`backend/internal/oauth/oauth2/tokenservice/utils.go:277`).

Both claim sources are gated on the same value. `buildClaimsFromScopes` returns an empty map
when the allowlist is empty (`utils.go:329`), and so does `buildClaimsFromRequest`
(`utils.go:363`, commented *"Return empty if no allowed attributes configured"*). The contract is
stated on `BuildClaims` itself: *"Returns empty if allowedUserAttributes is not configured."*

That allowlist comes from the application's `UserInfo.UserAttributes`
(`userinfo/service.go:429`). For a DCR-registered client it is never set:
`buildUserInfoConfig` (`backend/internal/oauth/oauth2/dcr/service.go:271`) returns `nil` unless
the registration requested a UserInfo signing or encryption algorithm, and even when it does
return a config it populates only `ResponseType` and the algorithm fields, never
`UserAttributes`.

So this is a configuration gap rather than a missing feature, and it is wider than the `claims`
parameter: it is also why the six scope modules report missing claims. Those have a separate
cause as well, since the Person schema stores `mobile_number` rather than `phone_number` and has
no `address` attribute, and that is tracked separately. Both need fixing for the scope modules to
pass; only this one blocks the `claims` parameter.

Discovery advertises the parameter unconditionally as `ClaimsParameterSupported: true`
(`backend/internal/oauth/oauth2/discovery/service.go`, in `GetOIDCMetadata`), which is accurate
about the implementation and misleading about the default deployment.

### Steps to Reproduce

1. Register a client through dynamic client registration, without requesting any UserInfo
   signing or encryption algorithm.
2. Complete an authorization code flow for that client with `scope=openid profile` and a claims
   request naming an attribute the user has:

   ```
   GET /oauth2/authorize
     ?client_id=<id>
     &scope=openid%20profile
     &claims={"userinfo":{"name":{"essential":true}}}
     &response_type=code&redirect_uri=<uri>&state=<state>&nonce=<nonce>
   ```

3. Exchange the code and call `GET /oauth2/userinfo` with the access token.

The response contains `sub` and nothing else, even though the user has a `name` attribute and
`name` is within the `profile` scope. Registering the same client with an explicit
`UserInfo.UserAttributes` list, or configuring an application through the Console with user
attributes selected, returns the claims as expected.

Found running the OpenID Foundation conformance suite, Basic OP plan, where the suite registers
its clients through DCR. `oidcc-claims-essential` requests `{"userinfo":{"name":{"essential":
true}}}` and reports:

```
WARNING  name not found in userinfo
```

### Suggested Improvement

Populate `UserInfo.UserAttributes` at registration from the scopes the client registered for, so
a DCR client behaves like a Console-configured one without extra setup. `StandardOIDCScopes`
(`backend/internal/oauth/oauth2/constants/constants.go:206`) already maps each standard scope to
its claims, so the derivation follows the grant rather than introducing a separate list. A client
registering for `openid profile email` would get the `profile` and `email` claims released, and
nothing beyond them.

The underlying decision is what an empty allowlist should mean: release nothing, or release what
the granted scopes imply. Fail-closed is defensible for a Console-configured application, where
an operator picks attributes deliberately. It reads differently for DCR, where nobody is there to
pick, and the client has already stated its intent by registering for a scope and being granted
it. Requiring a second, separate declaration of the same intent is what produces the current
outcome, where a client granted `profile` receives only `sub`.

Two implementation notes:

- `buildUserInfoConfig` (`backend/internal/oauth/oauth2/dcr/service.go:271`) returns `nil` unless
  the registration requested a UserInfo signing or encryption algorithm, so a DCR client has no
  `UserInfoConfig` to carry attributes on. That shape needs changing first.
- RFC 7591 §2 also defines `claims` as a registration parameter, letting a client declare the
  claims it wants at registration time. Supporting it would be a spec-defined way for a client to
  narrow or widen the set explicitly, and is worth considering alongside the scope-derived
  default rather than instead of it: the conformance suite does not send it, so it would not by
  itself change any result here.
