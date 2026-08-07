from __future__ import annotations

from typing import Any

from hr_agent.agents.parse_heuristics import needs_human_after_parse
from hr_agent.agents.parse_react import run_parse_correct
from hr_agent.nodes.questions import generate_questions
from hr_agent.nodes.screen import screen_candidate
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
    if needs_human:
        return {
            "profile": profile.model_dump(),
            "screen": {},
            "questions": [],
            "needs_human": True,
            "rejected": False,
            "error": reason,
            "raw_text": raw,
        }
    screen = screen_candidate(profile, jd or {})
    rejected = bool(screen.hard_fail_reasons) or screen.weighted_total < 50
    return {
        "profile": profile.model_dump(),
        "screen": screen.model_dump(),
        "questions": [],
        "needs_human": False,
        "rejected": rejected,
        "error": "",
        "raw_text": raw,
    }


def run_generate_questions(
    application_id: str,
    profile: dict[str, Any],
    jd: dict[str, Any],
) -> dict[str, Any]:
    _ = application_id
    p = CandidateProfile.model_validate(profile or {})
    qs = generate_questions(p, jd or {})
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
    """Legacy: parse+screen only (questions deferred to confirm)."""
    return run_parse_screen(application_id, resume_path, jd, resume_text)
