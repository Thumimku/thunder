# Title

No ACR values are configured out of the box, so `acr_values_supported` is absent from discovery and no `acr` claim is ever returned

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

Authentication context class handling is implemented end to end, but nothing is configured to
use it, so the capability is invisible and unusable in a default deployment.

The mechanism is complete. `acr_values_supported` is built from the `oauth.auth_class.acr_amr`
configuration map (`getSupportedAcrValues`,
`backend/internal/oauth/oauth2/discovery/service.go:222`), the flow engine reports the context it
satisfied as a `completed_auth_class` claim which is parsed into `CompletedACR`
(`backend/internal/oauth/oauth2/utils/assertion.go:88`), and the ID token builder emits it as
`acr` when it is present (`backend/internal/oauth/oauth2/tokenservice/builder.go:445`). The
authorization code and CIBA paths both carry it
(`backend/internal/oauth/oauth2/granthandlers/authorization_code.go:200`,
`ciba.go:262`).

The gap is that `acr_amr` is empty in a default deployment. That has two consequences:

1. `getSupportedAcrValues` returns an empty slice, and because the field is `omitempty`
   (`backend/internal/oauth/oauth2/discovery/model.go:45`), `acr_values_supported` is dropped
   from the discovery document entirely rather than published as an empty list.
2. No flow can report a `completed_auth_class` that maps to a configured ACR, so `CompletedACR`
   stays empty and the ID token never carries an `acr` claim.

A client therefore cannot discover which authentication contexts the server offers, cannot
request one meaningfully, and cannot verify which one was used.

This was found running the OpenID Foundation conformance suite, Basic OP plan. The relevant
module first records that discovery advertises nothing:

```
[SUCCESS] OIDCCAddAcrValuesToAuthorizationEndpointRequest: server discovery document does
not contain acr_values_supported (or, for static server config, test configuration does not
contain acr_values) so setting acr_values in authorization endpoint request to ...
   acr_values: 1 2
```

Having found no advertised values, the suite falls back to requesting `1 2`, and then reports:

```
[WARNING] ValidateIdTokenACRClaimAgainstAcrValuesRequest: An acr value was requested using
acr_values, so the server 'SHOULD' return an acr claim, but it did not.
```

OIDC Core §3.1.2.1 makes returning `acr` a SHOULD rather than a MUST, and §3.1.2.1 also treats
`acr_values` as a preference rather than a constraint, so not honouring an unrecognised value is
defensible on its own. The reportable part is that no value is recognisable, because none is
configured.

### Suggested Improvement

Ship a default `oauth.auth_class.acr_amr` mapping so the capability works without operator
setup, covering the authentication methods ThunderID already implements. Password would map to
`pwd`, and the OTP and passkey executors have natural AMR values, so the mapping can follow the
methods that already exist rather than inventing a scheme.

Two smaller points worth settling with it:

- Decide whether `acr_values_supported` should be published as an empty array rather than
  omitted when nothing is configured. Omitting it is not wrong, but an explicit empty list tells
  a client the server understands the concept and currently offers no contexts, which is more
  informative than silence.
- OIDC Core §5.2 also lets a client request `acr` through the `claims` parameter. The suite
  confirmed it did not exercise that path here (`ValidateIdTokenACRClaimAgainstRequest: Nothing
  to check; the conformance suite did not request an acr claim in request object`), and the
  `claims` parameter has a separate open issue about not affecting responses, so the two should
  end up consistent.

Because the underlying implementation is already present, this is closer to configuration and
defaults work than to a new feature.
