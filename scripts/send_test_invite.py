# -*- coding: utf-8 -*-
"""Create one application (sends invite email only; does not accept reply)."""
import json
import os
import urllib.request
from pathlib import Path

BASE = os.getenv("GO_API_BASE", "http://127.0.0.1:8080")
email = os.getenv("TEST_CANDIDATE_EMAIL", "g18949252323@outlook.com")
resume = Path(__file__).resolve().parents[1] / "samples" / "resume_zhangsan.txt"
text = resume.read_text(encoding="utf-8")
payload = {
    "jd_id": "jd-backend-001",
    "candidate_email": email,
    "candidate_name": "Zhang San",
    "resume_text": text,
}
data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
req = urllib.request.Request(
    BASE + "/v1/applications",
    data=data,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req, timeout=120) as resp:
    out = json.loads(resp.read().decode("utf-8"))
print("application_id:", out.get("application_id"))
print("status:", (out.get("application") or {}).get("status"))
