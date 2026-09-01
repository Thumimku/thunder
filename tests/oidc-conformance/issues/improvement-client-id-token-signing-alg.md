# Title

Let a client select its ID token signing algorithm via `id_token_signed_response_alg`

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

The server signs ID tokens with a single algorithm chosen server-side. A client cannot request
one, even though the server supports more than one and says so in discovery:

```
id_token_signing_alg_values_supported: ["RS256", "ES256"]
```

`id_token_signed_response_alg` is defined by OIDC Registration §2 for exactly this purpose, and
dynamic client registration accepts the parameter, but the value is discarded: it does not
appear in the registration response and has no effect on the tokens the client subsequently
receives. Silently accepting it is tracked separately as a bug; this issue is the missing
capability behind it.

The signing machinery itself is not the gap. Multiple algorithms are already supported and
advertised, ID token construction runs through
`backend/internal/oauth/oauth2/tokenservice/builder.go`, and `jose/jwt` takes an `alg` override
(`backend/internal/system/jose/jwt/service.go:97`). What is missing is per-client selection:
storing the client's choice at registration and honouring it at signing time.

Two Dynamic OP modules test this, `oidcc-idtoken-rs256` and `oidcc-userinfo-rs256`. Neither has
been run: that profile is blocked earlier by the absence of RFC 7592 management. So this
limitation is established from the discovery document and the DCR response rather than from a
conformance failure.

The equivalent for UserInfo, `userinfo_signed_response_alg`, has the same gap.
`userinfo_signing_alg_values_supported` is advertised in
`backend/internal/oauth/oauth2/discovery/service.go`, and a client cannot select from it either.

### Suggested Improvement

Accept `id_token_signed_response_alg` at registration, persist it with the client, and use it
when building that client's ID tokens, falling back to the server default when the client did
not ask for one. Validate the requested value against
`id_token_signing_alg_values_supported` at registration and reject an unsupported one with
`invalid_client_metadata`, rather than accepting and ignoring it.

Handle `userinfo_signed_response_alg` the same way, since it is the same shape of change against
an already-advertised capability list.

Both values should appear in the registration response once stored, so a client can confirm what
the server kept, matching how `logo_uri` and the other retained metadata already behave.
