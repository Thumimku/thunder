# Title

The conformance harness cannot complete modules that wait for a screenshot, reporting four passing modules as failures

# Type

Improvement (`.github/ISSUE_TEMPLATE/improvement.yml`, `Type/Improvement`)

---

### Current Limitation

`tests/oidc-conformance/browser_driver.py` visits the front-channel URLs a module publishes and
signs in, which is what allows the Basic OP plan to run at all (the suite's own HtmlUnit browser
cannot render the gate login page). It does not handle the other thing a module can wait for: a
screenshot upload, or confirmation that a page rendered.

Four modules complete every assertion and then sit in `WAITING` until
`MODULE_TIMEOUT_SECONDS` retires them, so they are reported as failures despite having found
nothing wrong:

| Module | Last recorded state |
|---|---|
| `oidcc-prompt-login` | Zero failures. Passes `CheckSecondIdTokenAuthTimeIsLaterIfPresent: auth_time is later in the second id_token`, then waits. |
| `oidcc-max-age-1` | Zero failures. Same assertion passes, then waits. |
| `oidcc-ensure-registered-redirect-uri` | `[REVIEW] ExpectRedirectUriErrorPage: Show redirect URI error page` |
| `oidcc-ensure-request-object-with-redirect-uri` | `[REVIEW] ExpectRedirectUriErrorPage: Show redirect URI error page` |

The first two are the clearest case: querying the suite's log API for those instances returns no
`FAILURE` or `WARNING` entries at all. They are reported as failures purely because the harness
cannot finish them.

This inflates the failure count from seven to nine and, more importantly, hides two modules that
exercise session reuse and re-authentication. Those are exactly the modules whose behaviour will
change when the authorization endpoint starts consulting the SSO session, so they are the ones
worth being able to measure.

Note that a real certification submission is interactive regardless, because the same screenshots
need human sign-off. The goal here is a trustworthy nightly signal, not an unattended
certification.

### Suggested Improvement

Teach the driver to satisfy a module's review placeholders as well as its front-channel URLs:

1. Detect a module waiting on a screenshot or visual-confirmation placeholder. The suite exposes
   this through the same `GET /api/runner/browser/{id}` response the driver already polls, which
   returns `show_qr_code`, `urls`, `visited` and `runners`; the placeholder itself appears in the
   module's log as a `REVIEW` entry with a placeholder id.
2. Capture the page with Playwright's screenshot API at the point the placeholder is raised, and
   upload it to the endpoint the suite provides for placeholder fulfilment, so the module can
   reach a terminal state.
3. Distinguish a module that is genuinely stalled from one waiting on a placeholder, so the
   `TIMED_OUT` result keeps meaning something.

Until this lands, read those four modules as "not measured" rather than as failures. The
`Results` section of `tests/oidc-conformance/README.md` records this so the numbers are not
misread later.
