from __future__ import annotations

from typing import Any

from hr_agent.agents.parse_heuristics import needs_human_after_parse
from hr_agent.agents.parse_react import run_parse_correct
from hr_agent.agents.parse_verify import verify_parse_profile
from hr_agent.nodes.questions import generate_questions
from hr_agent.nodes.screen import screen_candidate
from hr_agent.nodes.screen_tier import classify_screen
from hr_agent.state.models import CandidateProfile


def run_parse_screen(
    application_id: str,
    resume_path: str,
    jd: dict[str, Any],
    resume_text: str = "",
) -> dict[str, Any]:
    _ = application_id
    raw, profile = run_parse_correct(resume_path, resume_text)
    needs_human, reason = needs_human_after_parse(raw, profile)
    human_code = "parse_low_confidence" if needs_human else ""
    if needs_human:
        return {
            "profile": profile.model_dump(),
            "screen": {},
            "questions": [],
            "needs_human": True,
            "rejected": False,
            "error": reason,
            "human_reason_code": human_code,
            "screen_tier": "",
            "raw_text": raw,
        }

    v_need, v_code, v_detail = verify_parse_profile(raw, profile)
    if v_need:
        msg = "解析双路校验不一致，需人工复核" if v_code == "parse_cross_vendor_mismatch" else "解析校验未通过，需人工复核"
        return {
            "profile": profile.model_dump(),
            "screen": {},
            "questions": [],
            "needs_human": True,
            "rejected": False,
            "error": msg,
            "human_reason_code": v_code or "parse_inconsistent",
            "screen_tier": "",
            "parse_verify_detail": v_detail,
            "raw_text": raw,
        }

    screen = screen_candidate(profile, jd or {})
    tier, needs_human, rejected, code = classify_screen(screen)
    if needs_human:
        return {
            "profile": profile.model_dump(),
            "screen": screen.model_dump(),
            "questions": [],
            "needs_human": True,
            "rejected": False,
            "error": "筛选灰区，需 HR 人工判定",
            "human_reason_code": code or "screen_gray_zone",
            "screen_tier": tier,
            "raw_text": raw,
        }
    return {
        "profile": profile.model_dump(),
        "screen": screen.model_dump(),
        "questions": [],
        "needs_human": False,
        "rejected": rejected,
        "error": "",
        "human_reason_code": "",
        "screen_tier": tier,
        "raw_text": raw,
    }


def run_generate_questions(
    application_id: str,
    profile: dict[str, Any],
    jd: dict[str, Any],
    bank_hints: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    _ = application_id
    p = CandidateProfile.model_validate(profile or {})
    qs = generate_questions(p, jd or {}, bank_hints=bank_hints)
    return {
        "questions": [q.model_dump() for q in qs],
        "error": "",
    }


def run_parse_screen_questions(
    application_id: str,
    resume_path: str,
    jd: dict[str, Any],
    resume_text: str = "",
) -> dict[str, Any]:
    return run_parse_screen(application_id, resume_path, jd, resume_text)
