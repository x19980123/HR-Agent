# -*- coding: utf-8 -*-
"""Extended integration: create/accept, reschedule, decline."""
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESUME = (ROOT / "samples" / "resume_zhangsan.txt").read_text(encoding="utf-8")
BASE = os.getenv("GO_API_BASE", "http://127.0.0.1:8080")
AGENT = os.getenv("PYTHON_AGENT_URL", "http://127.0.0.1:8000")
EMAIL = os.getenv("TEST_CANDIDATE_EMAIL", "g18949252323@outlook.com")


def get(path: str):
    url = path if path.startswith("http") else BASE + path
    with urllib.request.urlopen(url, timeout=120) as resp:
        return json.loads(resp.read().decode("utf-8"))


def post(path: str, payload: dict | None = None):
    data = json.dumps(payload or {}, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        BASE + path,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.loads(resp.read().decode("utf-8"))


def create_app(name_suffix: str = ""):
    created = post(
        "/v1/applications",
        {
            "jd_id": "jd-backend-001",
            "candidate_email": EMAIL,
            "candidate_name": f"Zhang San{name_suffix}",
            "resume_text": RESUME,
        },
    )
    app_id = created["application_id"]
    app = created.get("application") or get(f"/v1/applications/{app_id}")
    if app.get("status") == "needs_human":
        app = post(f"/v1/applications/{app_id}/human/approve", {})
    return app_id, app


def reply(app_id: str, text: str):
    body = f"{text}\n[thread:{app_id}]\n"
    return post(
        f"/v1/applications/{app_id}/replies",
        {"email_body": body, "thread_id": app_id},
    )


def main() -> int:
    fails = []
    print("== health ==")
    try:
        print("python:", get(AGENT + "/health"))
        print("go:", get(BASE + "/health"))
    except Exception as e:
        print("health FAIL:", e)
        return 1

    # --- Case 1: accept ---
    print("\n== CASE1 accept ==")
    try:
        app_id, app = create_app(" accept")
        print("id:", app_id, "status:", app.get("status"))
        assert app.get("status") == "awaiting_reply", app
        app = reply(app_id, "我接受面试安排")
        print("after:", app.get("status"), app.get("reply_intent"))
        assert app.get("status") == "confirmed", app
        print("CASE1 PASS")
    except Exception as e:
        fails.append(f"accept: {e}")
        print("CASE1 FAIL:", e)

    # --- Case 2: reschedule then select slot ---
    print("\n== CASE2 reschedule then pick slot ==")
    try:
        app_id, app = create_app(" reschedule")
        print("id:", app_id, "status:", app.get("status"))
        assert app.get("status") == "awaiting_reply", app
        app = reply(app_id, "希望改期到下周一下午")
        print("after reschedule:", app.get("status"), "count=", app.get("reschedule_count"))
        assert app.get("status") == "awaiting_reply", app
        assert int(app.get("reschedule_count") or 0) >= 1, app
        app = reply(app_id, "选2")
        print("after pick:", app.get("status"), app.get("reply_intent"))
        assert app.get("status") == "confirmed", app
        print("CASE2 PASS")
    except Exception as e:
        fails.append(f"reschedule: {e}")
        print("CASE2 FAIL:", e)

    # --- Case 3: decline ---
    print("\n== CASE3 decline ==")
    try:
        app_id, app = create_app(" decline")
        print("id:", app_id, "status:", app.get("status"))
        app = reply(app_id, "抱歉，我拒绝这次面试")
        print("after:", app.get("status"), app.get("reply_intent"))
        assert app.get("status") == "declined", app
        print("CASE3 PASS")
    except Exception as e:
        fails.append(f"decline: {e}")
        print("CASE3 FAIL:", e)

    print("\n== summary ==")
    if fails:
        for f in fails:
            print("FAIL:", f)
        return 1
    print("ALL PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
