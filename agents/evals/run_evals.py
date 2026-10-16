"""Run the agent eval set against a running TickerLens API and report the
pass rate.

    uv run python evals/run_evals.py                 # all cases
    uv run python evals/run_evals.py V AAPL          # some tickers
    uv run python evals/run_evals.py --refresh       # ignore cached reports

Each case requests a report, waits for it, and checks:
- all three agents passed validation (verbatim quotes, no unsupported numbers);
- business-quality answers fall in the expected sets;
- optional phase and segment expectations.

Exit code is 0 unless --strict is given, so CI can report without failing.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path
from typing import Any

import httpx

HERE = Path(__file__).resolve().parent
# Required agents. Segments are best-effort (many companies disclose none);
# a case checks them only through "segmentsInclude".
AGENTS = ("business_overview", "business_quality")


def run_case(api: httpx.Client, case: dict[str, Any], refresh: bool, timeout_s: float) -> list[str]:
    """Returns the list of failed checks (empty = pass)."""
    created = api.post("/api/reports", json={"ticker": case["ticker"], "refresh": refresh})
    if created.status_code >= 400:
        return [f"could not create report: HTTP {created.status_code} {created.text[:200]}"]
    report_id = created.json()["id"]

    deadline = time.monotonic() + timeout_s
    while True:
        rep = api.get(f"/api/reports/{report_id}").json()
        if rep["status"] in ("done", "partial", "failed"):
            break
        if time.monotonic() > deadline:
            return [f"timed out after {timeout_s:.0f}s at stage {rep['stage']}"]
        time.sleep(5)

    if rep["status"] == "failed":
        return [f"report failed: {rep.get('error')}"]
    body = rep.get("report") or {}
    failures: list[str] = []

    for name in AGENTS:
        sec = (body.get("agents") or {}).get(name)
        if not sec or sec.get("status") != "ready":
            errors = "; ".join((sec or {}).get("errors") or ["missing"])
            failures.append(f"{name} failed: {errors[:200]}")

    answers = (body.get("businessQuality") or {}).get("answers") or {}
    for question, allowed in case.get("answers", {}).items():
        got = answers.get(question)
        if got not in allowed:
            failures.append(f"{question} = {got!r}, expected one of {allowed}")

    if "phase" in case:
        got = ((body.get("metrics") or {}).get("phase") or {}).get("name")
        if got != case["phase"]:
            failures.append(f"phase = {got!r}, expected {case['phase']!r}")

    names = [s["name"] for s in (body.get("segments") or {}).get("segments") or []]
    for want in case.get("segmentsInclude", []):
        if not any(want.lower() in n.lower() for n in names):
            failures.append(f"segments {names} do not include {want!r}")
    return failures


def main() -> int:
    p = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    p.add_argument("tickers", nargs="*", help="only run these tickers")
    p.add_argument("--api", default="http://localhost:8080")
    p.add_argument(
        "--refresh", action="store_true", help="rebuild reports instead of reusing recent ones"
    )
    p.add_argument("--timeout", type=float, default=600, help="seconds to wait per report")
    p.add_argument("--strict", action="store_true", help="exit 1 if any case fails")
    args = p.parse_args()

    cases = json.loads((HERE / "cases.json").read_text())["cases"]
    if args.tickers:
        wanted = {t.upper() for t in args.tickers}
        cases = [c for c in cases if c["ticker"] in wanted]

    results = []
    with httpx.Client(base_url=args.api, timeout=60) as api:
        for case in cases:
            t0 = time.monotonic()
            failures = run_case(api, case, args.refresh, args.timeout)
            elapsed = time.monotonic() - t0
            status = "PASS" if not failures else "FAIL"
            print(f"{status}  {case['ticker']:<6} {elapsed:6.0f}s")
            for f in failures:
                print(f"        - {f}")
            results.append({"ticker": case["ticker"], "pass": not failures, "failures": failures})

    passed = sum(r["pass"] for r in results)
    rate = passed / len(results) if results else 0
    print(f"\nPass rate: {passed}/{len(results)} ({rate:.0%})")
    (HERE / "results.json").write_text(json.dumps({"passRate": rate, "results": results}, indent=2))
    return 1 if args.strict and passed < len(results) else 0


if __name__ == "__main__":
    sys.exit(main())
