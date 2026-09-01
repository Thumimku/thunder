# Title

UserInfo omits standard claims for granted scopes because the Person schema stores `mobile_number` and has no `address`

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

A client that requests `profile`, `email`, `phone` or `address` does not receive all the claims
those scopes imply, so the UserInfo response is incomplete against OIDC Core §5.4. Six modules
of the Basic OP plan report this:

```
VerifyScopesReturnedInUserInfoClaims: 'claims' in userinfo doesn't contain all scope
items of scope in authorization request (corresponds to scope standard claims)
```

Affected: `oidcc-scope-profile`, `oidcc-scope-email`, `oidcc-scope-address`,
`oidcc-scope-phone`, `oidcc-scope-all`, `oidcc-alternate-happy-flow`. They are reported as
warnings rather than failures, matching how the suite grades certification, but the deviation is
real.

The scope-to-claim mapping is correct. `StandardOIDCScopes` in
`backend/internal/oauth/oauth2/constants/constants.go:206` maps `phone` to `phone_number` and
`phone_number_verified`, `address` to `address`, and `profile` to the §5.4 list, all as
specified.

The mismatch is with the built-in Person schema, which stores different attribute names:

| Scope | Standard claim | Person schema |
|---|---|---|
| `phone` | `phone_number` | `mobile_number` |
| `address` | `address` | not present |
| `profile` | `name`, `given_name`, `family_name`, `picture` | present |

So the mapping asks for claims the schema cannot supply, and no amount of correct mapping will
populate them.

The schema is strict about this: creating a user with a `phone_number` attribute is rejected
with `USR-1019` `error.userservice.schema_validation_failed`, so the attribute has to be written
as `mobile_number`.

The decision here reaches past the conformance suite. Any OIDC client requesting the `phone`
scope receives no phone claim, and any client requesting `address` receives no address, so this
affects real integrations rather than only the test profile.

### Suggested Improvement

Pick one of two directions and apply it consistently.

1. **Align the schema with the standard claim names.** Rename `mobile_number` to `phone_number`
   in the built-in Person schema and add `address` as a structured claim per OIDC Core §5.1.1.
   This makes the existing mapping work with no code change and gives clients the claims they
   ask for. It is a schema change with migration implications for existing deployments, which is
   the main cost.
2. **Map schema attributes onto standard claims.** Keep the schema names and translate at the
   claim-building layer, so `mobile_number` is emitted as `phone_number`. This avoids a
   migration but introduces a second place where claim names are decided, which needs to stay in
   step with `StandardOIDCScopes`.

Either way, `address` has no source attribute at all and needs one before the `address` scope can
return anything.

Worth deciding alongside `phone_number_verified` and `email_verified`, which the mapping also
names and the schema does not carry.
