# Copyright 2025 The ThunderID Authors
# SPDX-License-Identifier: Apache-2.0

"""Drive an OIDC conformance test plan against a running ThunderID server.

Creates the test plan through the conformance suite API, runs every module in it, waits for
each to reach a terminal state, and reports the outcome. Exits non-zero if any module fails.
"""

import argparse
import functools
import json
import os
import sys
import time

# Stream progress as it happens. Without this a run that is killed part way through loses
# every line it printed, because stdout is block-buffered when piped to a file or CI log.
print = functools.partial(print, flush=True)  # noqa: A001 - deliberate module-wide override

import httpx

from browser_driver import BrowserDriver

# The suite advertises itself as localhost.emobix.co.uk (resolves to 127.0.0.1) via
# --fintechlabs.base_url in docker-compose-dev.yml. Dialling the same name keeps its
# certificate valid and its issued redirect URIs consistent with what we call.
SUITE_URL = "https://localhost.emobix.co.uk:8443"
MODULE_TIMEOUT_SECONDS = 300
POLL_INTERVAL_SECONDS = 3

# The suite's DELETE /api/runner/{id} runs test.stop() via runInBackground and returns 200
# before the module has actually stopped. Every module in a plan shares one alias, and
# createTestAlias refuses to reassign it until the holder reaches FINISHED/INTERRUPTED, so
# starting the next module too eagerly gets a 409 and it never runs. Wait for the stop to land.
CANCEL_TIMEOUT_SECONDS = 60

# The locally running conformance suite serves a self-signed certificate over loopback.
LOCAL_SELF_SIGNED = False

# A module is done when it reaches one of these states. The suite also uses NOT_YET_CREATED,
# CREATED, CONFIGURED, RUNNING and WAITING while a module is still in flight.
TERMINAL_STATUSES = {"FINISHED", "INTERRUPTED"}

# Results the suite considers acceptable for a passing run. REVIEW and WARNING are surfaced in
# the summary but do not fail the build, matching how the suite itself grades certification runs.
# UNKNOWN is deliberately excluded: the suite uses it for "not yet known, probably still
# running", so treating it as a pass would mask a module that never actually finished.
PASSING_RESULTS = {"PASSED", "WARNING", "REVIEW", "SKIPPED"}


class ConformanceClient:
    """Thin wrapper over the conformance suite's REST API."""

    def __init__(self, base_url):
        self.base_url = base_url.rstrip("/")
        self.http = httpx.Client(verify=LOCAL_SELF_SIGNED, timeout=30)

    def create_plan(self, plan_name, variant, config):
        params = {"planName": plan_name}
        if variant:
            params["variant"] = json.dumps(variant)

        response = self.http.post(f"{self.base_url}/api/plan", params=params, json=config)
        response.raise_for_status()
        return response.json()

    def create_test_module(self, test_name, plan_id, variant=None):
        """Start one module.

        ``variant`` is the resolved per-module variant returned by the plan. It has to be
        passed back: the basic plan fixes client authentication per module (most modules use
        client_secret_basic, one uses client_secret_post), so dropping it would run that
        module with the wrong auth method and fail it spuriously.
        """
        params = {"test": test_name, "plan": plan_id}
        if variant:
            params["variant"] = json.dumps(variant)

        # A 409 means the shared plan alias is still held by a module that has not finished
        # stopping. That is transient, so retry rather than losing the module for the run.
        deadline = time.time() + CANCEL_TIMEOUT_SECONDS
        while True:
            response = self.http.post(f"{self.base_url}/api/runner", params=params)
            if response.status_code != 409 or time.time() >= deadline:
                break
            print("    alias still held, waiting to retry ...")
            time.sleep(POLL_INTERVAL_SECONDS)

        response.raise_for_status()
        return response.json()["id"]

    def get_module_info(self, module_id):
        response = self.http.get(f"{self.base_url}/api/info/{module_id}")
        response.raise_for_status()
        return response.json()

    def get_plan_info(self, plan_id):
        response = self.http.get(f"{self.base_url}/api/plan/{plan_id}")
        response.raise_for_status()
        return response.json()

    def get_log(self, module_id):
        response = self.http.get(f"{self.base_url}/api/log/{module_id}")
        response.raise_for_status()
        return response.json()

    def get_plan_variants(self, plan_name):
        """Return the variant keys the named plan declares as selectable.

        Plans differ: the basic plan asks the caller to choose server_metadata and
        client_registration, while the config plan declares none and pins them per module.
        Sending a variant a plan already fixes is a 400, so only send what it asks for.
        """
        response = self.http.get(f"{self.base_url}/api/plan/available")
        response.raise_for_status()
        for plan in response.json():
            if plan.get("planName") == plan_name:
                return set(plan.get("variants") or {})
        return set()

    def get_browser_status(self, module_id):
        """Return the module's front-channel state, or None while it has no browser yet."""
        try:
            response = self.http.get(f"{self.base_url}/api/runner/browser/{module_id}")
            response.raise_for_status()
            return response.json()
        except httpx.HTTPError:
            # 404 until the module is running, 503 until it has a BrowserControl.
            return None

    def mark_url_visited(self, module_id, url):
        """Tell the suite the URL has been visited so the module stops waiting on it."""
        try:
            self.http.post(
                f"{self.base_url}/api/runner/browser/{module_id}/visit", params={"url": url}
            )
        except httpx.HTTPError as error:
            print(f"    warning: could not mark {url} visited: {error}")

    def cancel_module(self, module_id):
        """Stop a module that is still running.

        Modules in a plan share one alias, and starting a second while the first is still
        live makes the suite kill the older one with an "alias conflict". A module abandoned
        at timeout would therefore corrupt whichever module runs next, so retire it here.
        """
        try:
            self.http.delete(f"{self.base_url}/api/runner/{module_id}")
        except httpx.HTTPError as error:
            print(f"    warning: could not cancel module {module_id}: {error}")
            return

        # The DELETE is asynchronous, so poll until the module actually releases the alias.
        deadline = time.time() + CANCEL_TIMEOUT_SECONDS
        while time.time() < deadline:
            try:
                status = self.get_module_info(module_id).get("status", "UNKNOWN")
            except httpx.HTTPError:
                time.sleep(POLL_INTERVAL_SECONDS)
                continue

            if status in TERMINAL_STATUSES:
                print(f"    cancelled (status={status})")
                return

            time.sleep(POLL_INTERVAL_SECONDS)

        print(
            f"    warning: module {module_id} did not stop within "
            f"{CANCEL_TIMEOUT_SECONDS}s; the next module may hit an alias conflict"
        )


def load_config(config_path, base_url):
    """Read the plan config template and substitute the runtime placeholders."""
    with open(config_path, encoding="utf-8") as handle:
        raw = handle.read()

    host = base_url.split("://", 1)[1].split(":")[0]
    raw = raw.replace("https://THUNDERID_HOST:8090", base_url)
    raw = raw.replace("THUNDERID_HOST", host)
    return json.loads(raw)


def run_module(client, test_name, plan_id, variant=None, driver=None):
    """Run one test module and return (result, status).

    When ``driver`` is set, front-channel URLs the module publishes are visited in a real
    browser between polls. See browser_driver.py for why the suite's own browser cannot do it.
    """
    print(f"\n>>> Running module: {test_name}")
    try:
        module_id = client.create_test_module(test_name, plan_id, variant)
    except httpx.HTTPError as error:
        print(f"    ERROR: could not start module: {error}")
        return "FAILED", "NOT_STARTED"

    deadline = time.time() + MODULE_TIMEOUT_SECONDS
    status = "CREATED"
    result = "UNKNOWN"
    visited_urls = set()

    try:
        while time.time() < deadline:
            try:
                info = client.get_module_info(module_id)
            except httpx.HTTPError:
                time.sleep(POLL_INTERVAL_SECONDS)
                continue

            status = info.get("status", "UNKNOWN")
            result = info.get("result", "UNKNOWN")

            if status in TERMINAL_STATUSES:
                print(f"    status={status} result={result}")
                return result, status

            if driver is not None:
                driver.drive_pending(module_id, visited_urls)

            time.sleep(POLL_INTERVAL_SECONDS)

        print(f"    TIMEOUT after {MODULE_TIMEOUT_SECONDS}s (last status={status})")
        client.cancel_module(module_id)
        return "TIMED_OUT", status
    finally:
        # The module's browser session ends with the module, so its cookies do too. Without
        # this a run of 38 modules would hold 38 contexts open at once.
        if driver is not None:
            driver.release(module_id)


def summarise(outcomes):
    """Print a table of module outcomes and return the number of failures."""
    print("\n" + "=" * 78)
    print("OIDC CONFORMANCE RESULTS")
    print("=" * 78)

    failures = []
    for test_name, (result, status) in outcomes.items():
        marker = "PASS" if result in PASSING_RESULTS else "FAIL"
        if marker == "FAIL":
            failures.append(test_name)
        print(f"[{marker}] {test_name:<55} {result} ({status})")

    print("=" * 78)
    print(f"Total: {len(outcomes)}   Passed: {len(outcomes) - len(failures)}   Failed: {len(failures)}")
    print("=" * 78)
    return failures


def write_summary(outcomes, failures, plan_id):
    """Append a Markdown table to the GitHub Actions job summary, when running in CI."""
    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if not summary_path:
        return

    lines = [
        "## OIDC Conformance Results",
        "",
        f"Plan: `{plan_id}`",
        "",
        "| Result | Module | Detail |",
        "| --- | --- | --- |",
    ]
    for test_name, (result, status) in outcomes.items():
        icon = "✅" if result in PASSING_RESULTS else "❌"
        lines.append(f"| {icon} | `{test_name}` | {result} ({status}) |")

    lines += ["", f"**{len(outcomes) - len(failures)} passed, {len(failures)} failed**", ""]

    with open(summary_path, "a", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")


def main():
    parser = argparse.ArgumentParser(description="Run an OIDC conformance plan against ThunderID.")
    parser.add_argument("--suite-url", default=SUITE_URL)
    parser.add_argument("--base-url", required=True, help="ThunderID base URL, e.g. https://localhost:8090")
    parser.add_argument("--config", required=True, help="Path to plan-config.json")
    parser.add_argument("--plan", default="oidcc-basic-certification-test-plan")
    parser.add_argument("--username", default="conformance-user")
    parser.add_argument("--password", default="Conformance@123")
    parser.add_argument(
        "--client-registration",
        default="dynamic_client",
        help="Conformance suite client_registration variant.",
    )
    parser.add_argument(
        "--response-type",
        default="code",
        help="Conformance suite response_type variant, used by the dynamic profile.",
    )
    parser.add_argument("--results-file", default="conformance-results.json")
    parser.add_argument(
        "--no-browser",
        action="store_true",
        help="Do not drive front-channel URLs. Login-dependent modules will time out.",
    )
    parser.add_argument(
        "--headed",
        action="store_true",
        help="Run the browser with a visible window, for debugging a failing login locally.",
    )
    args = parser.parse_args()

    client = ConformanceClient(args.suite_url)
    config = load_config(args.config, args.base_url)

    # ThunderID advertises `code` only and supports the `query` response mode, which is exactly
    # what the basic certification profile exercises. Which of these the plan actually accepts
    # varies, so offer only the keys it declares: passing one it pins itself is a 400.
    declared = client.get_plan_variants(args.plan)
    candidate = {
        "server_metadata": "discovery",
        "client_registration": args.client_registration,
        # The dynamic profile selects a response type instead of the metadata/registration
        # pair. ThunderID advertises `code` only, so that is the one it can be run against.
        "response_type": args.response_type,
    }
    variant = {k: v for k, v in candidate.items() if k in declared}
    if not variant:
        print(f">>> Plan '{args.plan}' declares no selectable variants; creating it without one")

    print(f">>> Creating plan '{args.plan}' with variant {variant}")
    plan = client.create_plan(args.plan, variant, config)
    plan_id = plan["id"]

    # Plan modules are objects, not names: each carries the variant the plan resolved for it.
    modules = [(module["testModule"], module.get("variant")) for module in plan["modules"]]

    print(f">>> Plan created: {args.suite_url}/plan-detail.html?plan={plan_id}")
    print(f">>> Modules to run: {len(modules)}")

    outcomes = {}
    interrupted = False
    try:
        run_all(client, modules, plan_id, args, outcomes)
    except KeyboardInterrupt:
        # Ctrl-C or a timeout wrapper. Whatever ran is still worth reporting, so fall through
        # to the summary rather than losing it.
        interrupted = True
        print("\n>>> Interrupted. Reporting the modules that completed.")

    failures = summarise(outcomes)
    write_summary(outcomes, failures, plan_id)

    incomplete = [name for name, _ in modules if name not in outcomes]
    with open(args.results_file, "w", encoding="utf-8") as handle:
        json.dump(
            {
                "planId": plan_id,
                "planUrl": f"{args.suite_url}/plan-detail.html?plan={plan_id}",
                "modules": {name: {"result": r, "status": s} for name, (r, s) in outcomes.items()},
                "failed": failures,
                "notRun": incomplete,
                "interrupted": interrupted,
            },
            handle,
            indent=2,
        )

    if incomplete:
        print(f"\n{len(incomplete)} module(s) did not run: {', '.join(incomplete)}")
    if failures:
        print(f"\nFailed modules: {', '.join(failures)}")

    # An incomplete run is not a pass, however well the modules that did run behaved.
    if failures or incomplete:
        sys.exit(1)

    print("\nAll conformance modules passed.")


def run_all(client, modules, plan_id, args, outcomes):
    """Run every module in the plan, recording each outcome in ``outcomes`` as it completes.

    The caller owns the dict so that an interrupted run still reports everything that
    finished before the interruption.
    """
    if args.no_browser:
        for test_name, module_variant in modules:
            outcomes[test_name] = run_module(client, test_name, plan_id, module_variant)
        return

    # One browser for the whole plan; each visit gets a fresh context so no session
    # leaks between modules. Modules that assume a logged-out user would otherwise fail.
    with BrowserDriver(
        client,
        args.suite_url,
        args.username,
        args.password,
        headless=not args.headed,
    ) as driver:
        for test_name, module_variant in modules:
            outcomes[test_name] = run_module(client, test_name, plan_id, module_variant, driver)


if __name__ == "__main__":
    main()
