# Title

Authorization requests carrying a `request` object are processed on the query string alone, silently dropping `state` and `nonce`

# Type

Bug (`.github/ISSUE_TEMPLATE/bug.yml`, `Type/Bug`)

---

### Description

ThunderID does not implement JAR (RFC 9101), which is a legitimate position: OIDC Core §6 makes
request object support optional for an OP, and OIDC Discovery defines
`request_parameter_supported` so a server can advertise that it does not.

The problem is what happens instead. An authorization request that carries a `request`
parameter is neither honoured nor rejected. The parameter is ignored and the request proceeds
using only the query string, so any parameter that the client placed *inside* the request
object is lost. The client receives what looks like a successful authorization response that
silently violates its own request.

`state` and `nonce` are the two that matter. Both are security parameters: `state` is the
client's CSRF defence and `nonce` binds the ID token to the authorization request. A client
that sends them inside a request object gets a response with neither, and no error explaining
why.

Discovery makes this worse rather than better. `request_parameter_supported`,
`request_uri_parameter_supported` and `request_object_signing_alg_values_supported` are all
absent from `/.well-known/openid-configuration`, and none of them appear in
`backend/internal/oauth/oauth2/discovery/model.go`. The two booleans have opposite defaults in
OIDC Discovery §3:

| Field | Default when omitted |
|---|---|
| `request_parameter_supported` | `false` |
| `request_uri_parameter_supported` | **`true`** |

So omitting `request_parameter_supported` correctly says the `request` parameter is not
supported, but omitting `request_uri_parameter_supported` advertises that `request_uri` **is**
supported. A client reading discovery is therefore told it may pass a `request_uri`, does so,
and has it silently ignored.

`request_uri` is only recognised as a PAR handle
(`backend/internal/oauth/oauth2/par/`), so a `request_uri` pointing at a JWT is not resolved.

### Steps to Reproduce

Send an authorization request that carries its parameters in a `request` object rather than on
the query string:

```
GET /oauth2/authorize
  ?client_id=<id>
  &request=<unsigned JWT containing response_type, redirect_uri, scope, state, nonce>
```

The request succeeds and redirects to the client, but the authorization response carries no
`state`, and the ID token's `nonce` does not match the one inside the object. A `request_uri`
pointing at the same JWT is likewise not resolved, since `request_uri` is only recognised as a
PAR handle.

This was found running the OpenID Foundation conformance suite, Basic OP plan
(`oidcc-basic-certification-test-plan`). Two modules cover it.

`oidcc-unsigned-request-object-supported-correctly-or-rejected-as-unsupported` sends the
parameters in an unsigned JWT in `request`, with `state` and `nonce` inside the object, and
fails:

```
CheckStateInAuthorizationResponse: State was passed in request, but is missing from
response (or returned in the wrong place)
   expected: HXuVIfU2Qa
ValidateIdTokenNonce: Nonce values mismatch
   expected: u64iFelu1S
```

`oidcc-request-uri-unsigned-supported-correctly-or-rejected-as-unsupported` sends the same by
reference in `request_uri` and stalls after the redirect to the authorization endpoint.

The module names state the contract: *supported correctly **or rejected as unsupported***.
Either behaviour passes. Silently ignoring is the one outcome neither branch allows.

#### Expected

Two things, both small, and neither requires implementing JAR.

1. **Advertise the truth in discovery.** Add `request_uri_parameter_supported: false`, since
   the default when omitted is `true` and that is the opposite of what ThunderID does.
   `request_parameter_supported` can stay omitted, as its default is already `false`, though
   stating it explicitly costs nothing and removes the asymmetry for anyone reading the
   document.
2. **Reject rather than ignore.** An authorization request carrying a `request` parameter
   should be rejected with the error OIDC Core §6.1 defines for it, `request_not_supported`,
   and a `request_uri` that is not a PAR handle should be rejected with
   `request_uri_not_supported`. Both are returned to the client's `redirect_uri` in the
   ordinary way, so the client learns its request was not honoured.

The discovery fix stops well-behaved clients sending something that will be dropped. The
rejection covers clients that do not read discovery, and turns both conformance modules above
into passes.

Note that PAR issues its own `request_uri` values, so whatever wording is chosen should make
clear that `request_uri_parameter_supported: false` refers to client-supplied request objects by
reference, not to PAR handles.

### Version

v1.0.0 (`oidc-conformnace-suite` branch, distribution `thunderid-1.0.0-macos-arm64`)

### Environment Details (with versions)

macOS, SQLite (default deployment), server on `https://thunderid.conformance.test:8090`.
