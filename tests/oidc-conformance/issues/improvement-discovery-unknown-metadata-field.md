# Title

Discovery advertises `authorization_grant_profiles_supported`, which is not defined by any OIDC or OAuth specification

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

The discovery document contains a top-level field that no specification defines:

```
authorization_grant_profiles_supported
```

The Config OP profile flags it. `oidcc-discovery-endpoint-verification` validates the document
against RFC 8414 and OIDC Discovery (requirements `RFC8414-2`, `OIDCD-3`) and reports:

```
CheckForUnexpectedParametersInServerMetadata
unknown_properties: [{"property": "authorization_grant_profiles_supported",
                      "path": "$.authorization_grant_profiles_supported"}]
```

This is the only finding against the discovery document. Every other field validates: the
issuer, all endpoints, `jwks_uri`, and the registration and revocation endpoints. The module
passes with this one warning, which makes Config OP the profile closest to a submittable state.

RFC 8414 §2 does allow additional metadata, and the suite's own message acknowledges that
unknown properties may be legitimate extensions it has not been told about. So this is not
necessarily wrong. It is unexplained, and unrecognised top-level metadata can confuse strict
clients that validate the document against a schema.

### Suggested Improvement

Decide which of two cases applies, and make the document say so.

1. **It is a deliberate ThunderID extension.** Then it should be documented, and a conformance
   run can stop reporting it by listing the name in the test configuration's
   `server.allow_unexpected_metadata_fields`. If the field is intended for wider use, the
   suite's message invites raising it with the OpenID Foundation so the schema can recognise
   it. A registered extension name, or a
   vendor-namespaced one, would also be clearer than a name that reads like a standard field.
2. **It is vestigial.** Then remove it from the discovery response. Advertising a capability
   nothing consumes is a small correctness cost with no benefit.

Either resolution is small. The reason to settle it is that Config OP is otherwise clean, and
this is the only thing standing between the discovery document and a warning-free run.
