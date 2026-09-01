# Title

Dynamic client registration returns `201` for `sector_identifier_uri` and `id_token_signed_response_alg` but discards both

# Type

Bug (`.github/ISSUE_TEMPLATE/bug.yml`, `Type/Bug`)

---

### Description

`POST /oauth2/dcr/register` accepts two client metadata parameters that it does not implement,
answers `201 Created`, and omits them from the registration response. A client cannot tell
that the values had no effect: RFC 7591 §3.2.1 says the response returns the metadata the
server registered, so a caller reading the response back sees the fields missing and has to
infer why.

- **`sector_identifier_uri`** is not implemented anywhere in the backend. `grep -r sector
  --include='*.go' backend/internal/` returns nothing outside tests. OIDC Core §8.1 requires
  the server to fetch the URI and verify that every registered `redirect_uri` appears in the
  JSON array it serves; a client registering one reasonably assumes that validation happened.
- **`id_token_signed_response_alg`** is discarded, so a client cannot select its ID token
  signing algorithm even though the server supports more than one:
  `id_token_signing_alg_values_supported` advertises `RS256` and `ES256`.

Two of the accepted-and-kept fields show the contrast. `logo_uri`, `tos_uri`, `policy_uri`,
`jwks_uri` and `jwks` all round-trip correctly through
`backend/internal/oauth/oauth2/dcr/model.go`, so the response is otherwise a faithful record
of what was registered.

Whether to *implement* either feature is a separate question, tracked as improvements. This
issue is about accepting a parameter and silently dropping it, which is true regardless of
which way that decision goes. RFC 7591 §3.2.1 also defines `invalid_client_metadata` for a
registration request containing metadata the server cannot honour.

Note that `sector_identifier_uri` is only meaningful with pairwise subject identifiers, and
`subject_types_supported` advertises `public` only, so there is nothing for a sector identifier
to attach to today.

### Steps to Reproduce

Against a running server with dynamic client registration enabled, register a client naming
both parameters:

```
POST /oauth2/dcr/register
{
  "client_name": "dcr-probe",
  "redirect_uris": ["https://localhost.emobix.co.uk:8443/test/a/x/callback"],
  "grant_types": ["authorization_code"],
  "response_types": ["code"],
  "logo_uri": "https://example.com/logo.png",
  "tos_uri": "https://example.com/tos",
  "policy_uri": "https://example.com/policy",
  "jwks_uri": "https://example.com/jwks.json",
  "sector_identifier_uri": "https://example.com/sectors.json",
  "id_token_signed_response_alg": "RS256"
}
```

Response:

```
201 Created
  client_id: fpIquJvJb4kQycQ_k-KJnQ
  logo_uri: https://example.com/logo.png
  tos_uri: https://example.com/tos
  policy_uri: https://example.com/policy
  jwks_uri: https://example.com/jwks.json
  sector_identifier_uri: ABSENT
  id_token_signed_response_alg: ABSENT
```

Note the sector identifier URI above does not resolve, so a server implementing §8.1 would
have rejected the registration rather than accepting it.

#### Expected

A registration request naming metadata the server cannot honour should be rejected with
`invalid_client_metadata` identifying the offending parameter, rather than succeeding with the
value discarded. If either field is implemented later, it should appear in the registration
response like the fields that already round-trip.

### Version

v1.0.0 (`oidc-conformnace-suite` branch, distribution `thunderid-1.0.0-macos-arm64`)

### Environment Details (with versions)

macOS, SQLite (default deployment), server on `https://thunderid.conformance.test:8090` with
`oauth.dcr.insecure` enabled for anonymous registration.
