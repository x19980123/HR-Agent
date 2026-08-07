# -*- coding: utf-8 -*-
import json
import urllib.request
from pathlib import Path

RESUME = Path(r"d:\HR-Agent\samples\resume_zhangsan.txt").read_text(encoding="utf-8")
BASE = "http://127.0.0.1:8080"


def post(path: str, payload: dict):
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        BASE + path,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.loads(resp.read().decode("utf-8"))


def get(path: str):
    with urllib.request.urlopen(BASE + path, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


print("== create application ==")
created = post(
    "/v1/applications",
    {
        "jd_id": "jd-backend-001",
        "candidate_email": "zhangsan@example.com",
        "candidate_name": "Zhang San",
        "resume_text": RESUME,
    },
)
app_id = created.get("application_id")
print("application_id:", app_id)
print("warning:", created.get("warning"))
app = created.get("application") or get(f"/v1/applications/{app_id}")
print("status after create:", app.get("status"))
assert app_id, "missing application_id"
assert app.get("status") in {
    "awaiting_reply",
    "questions_ready",
    "needs_human",
    "rejected",
    "failed",
}, app

if app.get("status") == "rejected":
    raise SystemExit("FAIL: rejected by screen")
if app.get("status") == "failed":
    raise SystemExit(f"FAIL: pipeline failed: {app.get('error_message')}")
if app.get("status") == "needs_human":
    print("needs_human -> approve")
    app = post(f"/v1/applications/{app_id}/human/approve", {})
    print("status after approve:", app.get("status"))

# refresh
app = get(f"/v1/applications/{app_id}")
print("status before reply:", app.get("status"))
assert app.get("status") == "awaiting_reply", app

print("== accept reply ==")
body = f"I accept the interview schedule.\n[thread:{app_id}]\n"
# Chinese accept also
body = f"我接受面试安排\n[thread:{app_id}]\n"
app = post(f"/v1/applications/{app_id}/replies", {"email_body": body, "thread_id": app_id})
print("status after reply:", app.get("status"))
print("reply_intent:", app.get("reply_intent"))
assert app.get("status") == "confirmed", app
print("E2E PASS")
