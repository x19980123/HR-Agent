from __future__ import annotations

import re
from typing import Any

from hr_agent.tools.parse_docs import extract_text

EMAIL_RE = re.compile(r"[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}")

BLOCKLIST = {"example.com", "test.com", "localhost", "import.local"}


def _score_email(email: str, raw: str, pos: int) -> float:
    score = 1.0
    domain = email.split("@")[-1].lower()
    if domain in BLOCKLIST:
        score -= 5
    window = raw[max(0, pos - 40) : pos + len(email) + 40].lower()
    for kw in ("email", "邮箱", "e-mail", "mail"):
        if kw in window:
            score += 3
            break
    if pos < 800:
        score += 1
    return score


def extract_contact(resume_path: str = "", resume_text: str = "") -> dict[str, Any]:
    raw = extract_text(resume_path or "", resume_text or "")
    candidates: list[tuple[float, str]] = []
    for m in EMAIL_RE.finditer(raw):
        em = m.group(0).strip().lower()
        if "@" not in em:
            continue
        sc = _score_email(em, raw, m.start())
        candidates.append((sc, em))
    candidates.sort(key=lambda x: (-x[0], x[1]))
    best_email = candidates[0][1] if candidates else ""
    name = ""
    for line in raw.splitlines()[:12]:
        line = line.strip()
        if not line or "@" in line or len(line) > 24:
            continue
        if any(k in line for k in ("简历", "电话", "手机", "邮箱", "Email")):
            continue
        name = line
        break
    conf = 0.5
    if best_email:
        conf = min(0.95, 0.55 + (candidates[0][0] * 0.08))
    return {
        "email": best_email,
        "name": name,
        "confidence": round(conf, 3),
        "method": "regex_rank",
        "candidates": [c[1] for c in candidates[:5]],
    }
