# -*- coding: utf-8 -*-
"""Remaining integration: LLM + public reply accept/reschedule/decline + questions-after-confirm."""
import json
import os
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESUME = (ROOT / "samples" / "resume_zhangsan.txt").read_text(encoding="utf-8")
BASE = os.getenv("GO_API_BASE", "http://127.0.0.1:8080")
AGENT = os.getenv("PYTHON_AGENT_URL", "http://127.0.0.1:8000")
EMAIL = os.getenv("TEST_CANDIDATE_EMAIL", "g18949252323@outlook.com")
HR_TOKEN = os.getenv("HR_API_TOKEN", "dev-hr-token-change-me")


def get(url: str, auth: bool = False):
    if not url.startswith("http"):
        url = BASE + url
    req = urllib.request.Request(url, method="GET")
    if auth:
        req.add_header("Authorization", "Bearer " + HR_TOKEN)
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.loads(resp.read().decode("utf-8"))


def post(base_path: str, payload: dict, root: str | None = None, auth: bool = False):
    root = root or BASE
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        root + base_path,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    if auth:
        req.add_header("Authorization", "Bearer " + HR_TOKEN)
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.loads(resp.read().decode("utf-8"))


def create_app(name: str):
    created = post(
        "/v1/admin/applications",
        {
            "jd_id": "jd-backend-001",
            "candidate_email": EMAIL,
            "candidate_name": name,
            "resume_text": RESUME,
        },
        auth=True,
    )
    app_id = created["application_id"]
    app = created.get("application") or get(f"/v1/admin/applications/{app_id}", auth=True)
    if app.get("status") == "needs_human":
        app = post(f"/v1/admin/applications/{app_id}/human/approve", {}, auth=True)
    return app_id, app


def reply_token(app_id: str) -> str:
    return get(f"/v1/admin/applications/{app_id}/reply-token", auth=True)["token"]


def main() -> int:
    fails = []
    print("== 1) health / LLM ==")
    h = get(AGENT + "/health")
    print(h)
    if h.get("llm_backend") == "heuristic":
        fails.append("LLM still heuristic (check OFFLINE_MODE / API key)")
    else:
        print("LLM backend OK:", h.get("llm_backend"))

    print("\n== 2) agent classify via DeepSeek ==")
    try:
        c = post(
            "/v1/pipeline/classify",
            {
                "application_id": "test",
                "email_body": "下周一下午不方便，想改期，谢谢",
                "context": {},
            },
            root=AGENT,
        )
        print(c)
        if c.get("intent") != "reschedule":
            fails.append(f"classify expected reschedule, got {c.get('intent')}")
        if c.get("llm_backend") == "heuristic":
            fails.append("classify used heuristic")
    except Exception as e:
        fails.append(f"classify: {e}")

    print("\n== 3) parse_screen + generate_questions ==")
    try:
        jd = {
            "id": "jd-backend-001",
            "title": "Backend Engineer",
            "requirements": {"years": 3, "skills": ["Go", "Python", "MySQL"]},
            "weights": {
                "education": 15,
                "major": 10,
                "years": 20,
                "skills": 35,
                "projects": 15,
                "papers": 5,
            },
        }
        out = post(
            "/v1/pipeline/parse_screen",
            {"application_id": "llm-test", "resume_text": RESUME, "jd": jd},
            root=AGENT,
        )
        print(
            "parse_screen backend=",
            out.get("llm_backend"),
            "rejected=",
            out.get("rejected"),
            "score=",
            (out.get("screen") or {}).get("weighted_total"),
            "q=",
            len(out.get("questions") or []),
        )
        if len(out.get("questions") or []) != 0:
            fails.append("parse_screen should not return questions")
        if not out.get("rejected"):
            qout = post(
                "/v1/pipeline/generate_questions",
                {
                    "application_id": "llm-test",
                    "profile": out.get("profile") or {},
                    "jd": jd,
                },
                root=AGENT,
            )
            print("generate_questions q=", len(qout.get("questions") or []))
            if len(qout.get("questions") or []) < 1:
                fails.append("generate_questions empty")
        if out.get("error"):
            fails.append(f"pipeline error: {out.get('error')}")
    except Exception as e:
        fails.append(f"pipeline: {e}")

    print("\n== 4) E2E public accept ==")
    try:
        app_id, app = create_app("LLM Feishu Test")
        print("create:", app_id, app.get("status"))
        tok = reply_token(app_id)
        out = post(f"/v1/public/reply/{tok}", {"action": "accept"})
        app = out.get("application") or {}
        print("accept:", app.get("status"), "q=", len(app.get("questions") or []))
        if app.get("status") != "confirmed":
            fails.append(f"accept status={app.get('status')}")
        if not (app.get("questions") or []):
            fails.append("no questions after accept")
    except Exception as e:
        fails.append(f"accept e2e: {e}")

    print("\n== 5) E2E public reschedule + pick ==")
    try:
        app_id, app = create_app("Reschedule Test")
        tok = reply_token(app_id)
        app = post(f"/v1/public/reply/{tok}", {"action": "reschedule"}).get("application") or {}
        print("reschedule:", app.get("status"), "count=", app.get("reschedule_count"))
        tok2 = reply_token(app_id)
        view = get(f"/v1/public/reply/{tok2}")
        print("slots:", len(view.get("slots") or []))
        app = post(f"/v1/public/reply/{tok2}", {"action": "pick_slot", "slot_index": 0}).get("application") or {}
        print("pick:", app.get("status"), app.get("reply_intent"))
        if app.get("status") != "confirmed":
            fails.append(f"reschedule final={app.get('status')}")
    except Exception as e:
        fails.append(f"reschedule: {e}")

    print("\n== 6) E2E public decline ==")
    try:
        app_id, app = create_app("Decline Test")
        tok = reply_token(app_id)
        app = post(f"/v1/public/reply/{tok}", {"action": "decline"}).get("application") or {}
        print("decline:", app.get("status"), app.get("reply_intent"))
        if app.get("status") != "declined":
            fails.append(f"decline status={app.get('status')}")
    except Exception as e:
        fails.append(f"decline: {e}")

    print("\n== summary ==")
    if fails:
        for f in fails:
            print("FAIL:", f)
        return 1
    print("ALL REMAINING TESTS PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
