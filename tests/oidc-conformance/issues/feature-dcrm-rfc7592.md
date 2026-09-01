# Title

Support dynamic client registration management (RFC 7592)

# Type

New Feature (`.github/ISSUE_TEMPLATE/feature.yml`, `Type/NewFeature`)

---

### What problem are we solving?

When an application registers itself with ThunderID through dynamic client registration, it
cannot afterwards read, update or delete that registration, which means a registered client can
never rotate its keys, correct its metadata or clean itself up, and every stale registration
accumulates permanently with no way to remove it.

`POST /oauth2/dcr/register` creates a client and returns `201`, but the response omits the two
fields RFC 7591 §3.2.1 defines for subsequent management:

```
POST /oauth2/dcr/register  ->  201
  client_id: fpIquJvJb4kQycQ_k-KJnQ
  registration_access_token: ABSENT
  registration_client_uri: ABSENT
```

Those two fields are what RFC 7592 §2 builds on: `GET`, `PUT` and `DELETE` against the
per-client `registration_client_uri`, authorised by the `registration_access_token` as a bearer
token. Without them, registration is a one-way door.

The token is the part that makes this work at all. A dynamic registrant is typically anonymous,
so there is no user and no admin credential in the exchange. The server hands back a capability
token at creation time, scoped to the single client it just created, and that token is the only
thing that can authorise later changes to it.

Two consequences are already visible. Every module of the Basic OP conformance plan ends by
trying to clean up after itself and failing:

```
UnregisterDynamicallyRegisteredClient: Couldn't find registration_access_token.
```

And the Dynamic OP certification profile cannot run at all. Of its 23 modules, several register
a client and then modify it: `oidcc-server-rotate-keys` and
`oidcc-refresh-token-rp-key-rotation` rotate the client's keys, which is a `PUT` to
`registration_client_uri`. They have nothing to call, so the profile is blocked before its own
feature coverage is reached.

### Who are we solving this for?

Primary: Developer integrating an application that self-registers
- Goal: Register once, then maintain the registration over the application's life, rotating keys
  and correcting metadata without operator involvement.
- Pain: A registration is immutable and permanent. Rotating a signing key means registering a
  second client and abandoning the first, which changes the `client_id` every consumer depends
  on. Nothing can remove the abandoned one.

Secondary: Operator of a ThunderID deployment with DCR enabled
- Goal: Keep the client list reflecting reality, and let registrants clean up after themselves.
- Pain: Registrations only accumulate. Removing a stale one is an out-of-band administrative
  action, and there is no way to tell which registrations are still in use.

Tertiary: ThunderID maintainers pursuing OIDC certification
- Goal: Run the Dynamic OP profile and see genuine results.
- Pain: The profile is blocked before it starts, so 19 modules' worth of behaviour that no other
  profile covers is entirely unmeasured.

### Why should we solve this now?

Why now:
- Value to Persona: Key rotation becomes possible without changing `client_id`, which is the
  difference between a maintainable integration and one that has to be torn down and rebuilt.
- Value to Org: Unblocks a certification profile whose 23 modules cover registration metadata,
  redirect URI validation, asymmetric signing and key rotation, none of which the Basic profile
  reaches. It is also the smallest of the identified conformance gaps relative to what it opens
  up.
- Security posture: `oauth.dcr.insecure` is currently all-or-nothing. Either any anonymous
  caller may register, or a caller must hold an admin permission. A registration access token is
  a third and better-scoped position: whoever created a client may manage that client and
  nothing else. Settling that shape now is easier than retrofitting it once management endpoints
  exist.
- Cost of delay: Every deployment with DCR enabled is accruing registrations that cannot be
  removed. That is a growing cleanup problem rather than a static one, and the longer clients
  work around immutable registrations by creating new ones, the more `client_id` churn ends up
  baked into integrations.

### Proposed Solution

Implement RFC 7592 client configuration management for dynamically registered clients.

1. **Issue and store a registration access token.** Generate one at registration, store it
   against the client, and return it with a `registration_client_uri` in the registration
   response, per RFC 7591 §3.2.1.
2. **Serve the three management operations** at that URI:
   - `GET` returns the current registration.
   - `PUT` replaces the client metadata, per RFC 7592 §2.2, returning the stored result so a
     client can confirm what the server kept. The round-trip behaviour that `logo_uri`,
     `tos_uri`, `policy_uri`, `jwks_uri` and `jwks` already have in
     `backend/internal/oauth/oauth2/dcr/model.go` is the pattern to follow.
   - `DELETE` deregisters the client.
3. **Authorise each operation against the stored token**, so a registrant can act only on the
   client it created. RFC 7592 §3 requires rejecting a token that does not match the client in
   the URI.
4. **Decide the token's lifecycle.** RFC 7592 §2.2 allows the server to return a new
   registration access token from a `PUT`, rotating it. Whether to rotate, and whether the token
   expires at all, are the two decisions worth making explicitly rather than by default.

Scope note: this is deliberately separate from the two DCR parameters that are currently
accepted and discarded, `sector_identifier_uri` and `id_token_signed_response_alg`. Those are
tracked on their own and neither blocks this.

### Alternatives

- **Admin-only client management through the existing application API.** An operator can already
  manage applications, so a registrant could ask an administrator to make changes. This is the
  status quo for anything beyond registration, and it does not serve the primary persona at all:
  the point of dynamic registration is that no operator is involved. It also cannot satisfy the
  Dynamic OP modules, which authenticate as the registrant.
- **Let a client re-register and abandon the old registration.** Works today, and is what a
  client rotating a key has to do. It changes `client_id`, which breaks every consumer holding
  the old one, and it leaves an unremovable registration behind. This is the workaround whose
  cost the feature removes.
- **Implement only `DELETE`.** Cheaper, and it would fix the conformance cleanup warning and the
  accumulation problem. It would not enable key rotation, which is the substantive use case, and
  the token and URI machinery is the same either way, so most of the work is shared.
- **Do nothing and skip Dynamic OP certification.** Defensible if that profile is not a goal.
  It leaves the accumulation problem and the immutable-registration limitation in place for every
  DCR deployment regardless of certification.
