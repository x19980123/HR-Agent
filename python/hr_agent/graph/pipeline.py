from __future__ import annotations

"""LangGraph pipeline with sequential fallback when langgraph is unavailable."""

from typing import Any


def run_parse_screen(
    application_id: str,
    resume_path: str,
    jd: dict[str, Any],
    resume_text: str = "",
) -> dict[str, Any]:
    try:
        from hr_agent.graph.pipeline_langgraph import run_parse_screen as _run

        return _run(application_id, resume_path, jd, resume_text)
    except ImportError:
        from hr_agent.graph.pipeline_fallback import run_parse_screen as _run

        return _run(application_id, resume_path, jd, resume_text)


def run_generate_questions(
    application_id: str,
    profile: dict[str, Any],
    jd: dict[str, Any],
) -> dict[str, Any]:
    try:
        from hr_agent.graph.pipeline_langgraph import run_generate_questions as _run

        return _run(application_id, profile, jd)
    except ImportError:
        from hr_agent.graph.pipeline_fallback import run_generate_questions as _run

        return _run(application_id, profile, jd)


def run_parse_screen_questions(
    application_id: str,
    resume_path: str,
    jd: dict[str, Any],
    resume_text: str = "",
) -> dict[str, Any]:
    return run_parse_screen(application_id, resume_path, jd, resume_text)
