# -*- coding: utf-8 -*-
"""Full integration test: create -> awaiting_reply -> public accept -> confirmed + questions."""
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESUME = (ROOT / "samples" / "resume_zhangsan.txt").read_text(encoding="utf-8")
BASE = os.getenv("GO_API_BASE", "http://127.0.0.1:8080")
AGENT = os.getenv("PYTHON_AGENT_URL", "http://127.0.0.1:8000")
HR_TOKEN = os.getenv("HR_API_TOKEN", "dev-hr-token-change-me")


def get(path: str, auth: bool = False):
    url = path if path.startswith("http") else BASE + path
    req = urllib.request.Request(url, method="GET")
    if auth:
        req.add_header("Authorization", "Bearer " + HR_TOKEN)
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.loads(resp.read().decode("utf-8"))


def post(path: str, payload: dict | None = None, auth: bool = False):
    data = json.dumps(payload or {}, ensure_ascii=False).encode("utf-8")
    url = path if path.startswith("http") else BASE + path
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    if auth:
        req.add_header("Authorization", "Bearer " + HR_TOKEN)
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main() -> int:
    failures = []

    print("== health ==")
    try:
        py = get(AGENT + "/health")
        print("python:", py)
    except Exception as e:
        failures.append(f"python health: {e}")
        print("python health FAIL:", e)

    try:
        go = get(BASE + "/health")
        print("go:", go)
    except Exception as e:
        failures.append(f"go health: {e}")
        print("go health FAIL:", e)

    if failures:
        print("\nABORT: services not ready")
        return 1

    print("\n== create application (parse/screen only, then schedule) ==")
    try:
        created = post(
            "/v1/admin/applications",
            {
                "jd_id": "jd-backend-001",
                "candidate_email": os.getenv("TEST_CANDIDATE_EMAIL", "zhangsan@example.com"),
                "candidate_name": "Zhang San",
                "resume_text": RESUME,
            },
            auth=True,
        )
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print("create HTTP", e.code, body[:500])
        return 1

    app_id = created.get("application_id")
    warn = created.get("warning") or ""
    print("application_id:", app_id)
    if warn:
        print("warning:", warn[:300])

    if not app_id:
        print("FAIL: no application_id")
        return 1

    app = created.get("application") or get(f"/v1/admin/applications/{app_id}", auth=True)
    status = app.get("status")
    qs = app.get("questions")
    qcount = len(qs) if isinstance(qs, list) else 0
    print("status:", status)
    print("has profile:", bool(app.get("profile")))
    print("has screen:", bool(app.get("screen")))
    print("questions count (expect 0 before confirm):", qcount)

    if status == "failed":
        print("FAIL:", app.get("error_message"))
        return 1
    if status == "rejected":
        print("FAIL: rejected by screen", app.get("screen"))
        return 1
    if status == "needs_human":
        print("needs_human -> human approve")
        try:
            app = post(f"/v1/admin/applications/{app_id}/human/approve", {}, auth=True)
            status = app.get("status")
            print("after approve:", status)
        except urllib.error.HTTPError as e:
            failures.append(f"human approve: {e.read().decode()[:200]}")
            status = app.get("status")

    app = get(f"/v1/admin/applications/{app_id}", auth=True)
    if app.get("status") != "awaiting_reply":
        failures.append(f"expected awaiting_reply, got {app.get('status')}")
    else:
        print("OK awaiting_reply")
    if isinstance(app.get("questions"), list) and len(app["questions"]) > 0:
        failures.append("questions should be empty before confirm")

    print("\n== public reply accept ==")
    try:
        tok = get(f"/v1/admin/applications/{app_id}/reply-token", auth=True)
        token = tok["token"]
        print("reply url:", tok.get("url"))
        out = post(f"/v1/public/reply/{token}", {"action": "accept"})
        app = out.get("application") or get(f"/v1/admin/applications/{app_id}", auth=True)
    except urllib.error.HTTPError as e:
        print("reply FAIL:", e.read().decode()[:400])
        failures.append("public accept failed")
        app = get(f"/v1/admin/applications/{app_id}", auth=True)

    print("status:", app.get("status"), "intent:", app.get("reply_intent"))
    if app.get("status") != "confirmed":
        failures.append(f"expected confirmed, got {app.get('status')}")
    qs = app.get("questions") or []
    print("questions after confirm:", len(qs) if isinstance(qs, list) else qs)
    if not isinstance(qs, list) or len(qs) < 1:
        failures.append("expected questions after confirm")

    print("\n== summary ==")
    if failures:
        for f in failures:
            print("FAIL:", f)
        return 1
    print("ALL PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
