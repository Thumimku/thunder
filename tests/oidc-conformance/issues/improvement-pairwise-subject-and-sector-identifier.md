# Title

Support pairwise subject identifiers and `sector_identifier_uri`

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

ThunderID issues the same `sub` value to every client for a given user. Discovery advertises:

```
subject_types_supported: ["public"]
```

OIDC Core §8 defines a second subject type, `pairwise`, which derives a different `sub` per
client so that two clients cannot correlate a user by comparing identifiers. ThunderID does not
implement it, and `sector_identifier_uri` does not appear anywhere in the backend: `grep -r
sector --include='*.go' backend/internal/` returns nothing outside tests.

These are two features rather than one, and the order matters.

**Pairwise subject identifiers** are the substantive part. §8.1 requires the `sub` to be derived
so that it is stable per client (or per sector) and cannot be reversed to a shared identifier.
This is a privacy capability: with `public` subjects, any two clients a user signs into can
determine that they are the same person.

**`sector_identifier_uri`** exists only to group clients that should share one pairwise `sub`.
An organisation running `app1.example.com` and `app2.example.com` registers a sector identifier
so both receive the same `sub`, while remaining unlinkable to clients outside the sector.
Without pairwise subjects there is nothing for it to attach to, which is why implementing it
alone would be plumbing for a feature that does not exist.

The Basic OP profile does not require either. Two Dynamic OP modules test the sector identifier,
`oidcc-registration-sector-uri` and `oidcc-registration-sector-bad`, and neither has been run:
that profile is blocked earlier by the absence of RFC 7592 management. The second of those
expects a registration to be *rejected* when the sector URI's contents do not list the client's
own redirect URI, so partial support cannot pass it.

Separately, dynamic client registration currently accepts `sector_identifier_uri` and discards
it, returning `201` as though it took effect. That is tracked as a bug and is worth fixing
regardless of whether this improvement is scheduled.

### Suggested Improvement

Take it in two steps, and only if Dynamic OP certification or cross-client unlinkability is
wanted.

1. **Implement pairwise subject identifiers.** Add `pairwise` to `subject_types_supported`,
   support `subject_type` at client registration, and derive `sub` per §8.1 from a per-client
   (or per-sector) salt that cannot be reversed. Existing clients keep `public` so no issued
   identifier changes.
2. **Then implement `sector_identifier_uri`.** At registration, fetch the URI, parse the JSON
   array of redirect URIs, and verify that every `redirect_uri` on the registration appears in
   it, rejecting the registration with `invalid_client_metadata` when it does not. Use the
   sector host rather than the redirect URI host when deriving the pairwise `sub`.

The validation in step 2 is the security-relevant part: without it a client could claim any
sector and enter another organisation's `sub` space.

If neither Dynamic OP certification nor pairwise subjects are on the roadmap, the reasonable
outcome is to reject `sector_identifier_uri` at registration rather than accept it, and close
this as not planned.
