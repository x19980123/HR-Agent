# -*- coding: utf-8 -*-
"""Test Feishu personal calendar: create invite + accept."""
import json
import os
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESUME = (ROOT / "samples" / "resume_zhangsan.txt").read_text(encoding="utf-8")
BASE = os.getenv("GO_API_BASE", "http://127.0.0.1:8080")
EMAIL = os.getenv("TEST_CANDIDATE_EMAIL", "g18949252323@outlook.com")


def get(path: str):
    url = path if path.startswith("http") else BASE + path
    with urllib.request.urlopen(url, timeout=120) as resp:
        return json.loads(resp.read().decode("utf-8"))


def post(path: str, payload: dict):
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        BASE + path,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main() -> int:
    print("health:", get(BASE + "/health"))
    created = post(
        "/v1/applications",
        {
            "jd_id": "jd-backend-001",
            "candidate_email": EMAIL,
            "candidate_name": "Feishu Calendar Test",
            "resume_text": RESUME,
        },
    )
    app_id = created.get("application_id")
    app = created.get("application") or {}
    print("application_id:", app_id)
    print("status:", app.get("status"))
    print("warning:", created.get("warning") or "")
    print("error:", app.get("error_message") or "")
    if app.get("status") == "failed":
        return 1
    if app.get("status") == "needs_human":
        app = post(f"/v1/applications/{app_id}/human/approve", {})
        print("after approve:", app.get("status"))
    if app.get("status") != "awaiting_reply":
        print("unexpected status", app)
        return 1
    app = post(
        f"/v1/applications/{app_id}/replies",
        {
            "email_body": f"我接受面试安排\n[thread:{app_id}]\n",
            "thread_id": app_id,
        },
    )
    print("after accept:", app.get("status"), app.get("reply_intent"))
    return 0 if app.get("status") == "confirmed" else 1


if __name__ == "__main__":
    sys.exit(main())
