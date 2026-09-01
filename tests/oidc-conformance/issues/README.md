# Conformance findings, as draft issues

One file per issue, each ready to paste into a GitHub issue. Every file starts with a `# Title`
section, then a `# Type` line naming the template it follows, then the body under that
template's headings.

Findings come from a Basic OP and Config OP conformance run plus direct probing of the DCR
endpoint. The evidence behind each one is in the `Results` section of
`tests/oidc-conformance/README.md`. Delete this directory once the issues are filed.

## Bugs

Cases where the server accepts input and discards it, or derives a value from the wrong source.
These are defects independently of whether the related feature is ever implemented.

| File | Issue |
|---|---|
| `bug-request-object-ignored.md` | A `request` object is neither honoured nor rejected, so `state` and `nonce` inside it are silently dropped |
| `bug-dcr-drops-unsupported-metadata.md` | DCR returns `201` for `sector_identifier_uri` and `id_token_signed_response_alg` but discards both |
| `bug-auth-time-from-code-creation.md` | `auth_time` is taken from authorization code creation rather than the session's authentication time |

## Features

Substantial new capabilities, using `feature.yml` rather than `improvement.yml` because each is
a body of work rather than a change to existing behaviour.

| File | Issue |
|---|---|
| `feature-dcrm-rfc7592.md` | Support dynamic client registration management (RFC 7592) |

## Improvements

Changes to behaviour that already exists, and one gap in this harness.

| File | Issue |
|---|---|
| `improvement-authorization-endpoint-sso-session.md` | Consult the existing SSO session at the authorization endpoint so `prompt=none`, `id_token_hint` and `max_age` work |
| `improvement-userinfo-scope-claims.md` | UserInfo omits standard claims because the Person schema stores `mobile_number` and has no `address` |
| `improvement-claims-request-parameter.md` | The `claims` request parameter does not affect responses, though discovery advertises support |
| `improvement-client-id-token-signing-alg.md` | Let a client select its ID token signing algorithm |
| `improvement-acr-claim-for-acr-values.md` | No `acr` claim is returned when a client requests `acr_values` |
| `improvement-pairwise-subject-and-sector-identifier.md` | Support pairwise subject identifiers and `sector_identifier_uri` |
| `improvement-discovery-unknown-metadata-field.md` | Discovery advertises `authorization_grant_profiles_supported`, which no specification defines |
| `improvement-harness-screenshot-placeholders.md` | The harness cannot complete modules that wait for a screenshot, so four passing modules report as failures |

## Suggested order

Cheapest first, by effect on the conformance numbers rather than by effort alone.

1. `bug-request-object-ignored.md` — hours, and turns two Basic OP failures into passes.
2. `bug-dcr-drops-unsupported-metadata.md` — hours, stops two parameters being silently
   discarded.
3. `improvement-harness-screenshot-placeholders.md` — recovers four modules that already pass,
   giving a clean baseline before any product work is judged against it.
4. `bug-auth-time-from-code-creation.md` — small, and a prerequisite for the `max_age` behaviour
   in item 5.
5. `improvement-authorization-endpoint-sso-session.md` — the session subsystem already exists,
   so this is smaller than it first appears. Fixes three confirmed failures and probably two
   more.
6. `feature-dcrm-rfc7592.md` — self-contained, and unblocks the whole Dynamic OP profile.
7. `improvement-userinfo-scope-claims.md` — a naming decision more than an implementation, but
   it affects every OIDC client rather than only the suite, so it may deserve to be earlier.

The remainder are smaller or depend on a scoping decision. `improvement-pairwise-subject-and-sector-identifier.md`
in particular is only worth scheduling if Dynamic OP certification is a goal.
