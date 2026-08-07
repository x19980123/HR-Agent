from __future__ import annotations

from typing import Any, TypedDict

from langgraph.graph import END, START, StateGraph

from hr_agent.agents.parse_heuristics import needs_human_after_parse
from hr_agent.agents.parse_react import run_parse_correct
from hr_agent.nodes.questions import generate_questions
from hr_agent.nodes.screen import screen_candidate
from hr_agent.state.models import CandidateProfile


class PipelineState(TypedDict, total=False):
    application_id: str
    resume_path: str
    resume_text: str
    jd: dict[str, Any]
    raw_text: str
    profile: dict
    screen: dict
    questions: list[dict]
    needs_human: bool
    rejected: bool
    error: str


def node_parse(state: PipelineState) -> dict:
    raw, profile = run_parse_correct(state.get("resume_path", ""), state.get("resume_text", ""))
    needs_human, reason = needs_human_after_parse(raw, profile)
    return {
        "raw_text": raw,
        "profile": profile.model_dump(),
        "needs_human": needs_human,
        "rejected": False,
        "error": reason if needs_human else "",
    }


def node_screen(state: PipelineState) -> dict:
    profile = CandidateProfile.model_validate(state["profile"])
    screen = screen_candidate(profile, state.get("jd") or {})
    rejected = bool(screen.hard_fail_reasons) or screen.weighted_total < 50
    return {"screen": screen.model_dump(), "rejected": rejected}


def route_after_parse(state: PipelineState) -> str:
    if state.get("needs_human"):
        return "end_early"
    return "screen"


def build_parse_screen_graph():
    g = StateGraph(PipelineState)
    g.add_node("parse", node_parse)
    g.add_node("screen", node_screen)
    g.add_node("end_early", lambda s: {})
    g.add_edge(START, "parse")
    g.add_conditional_edges("parse", route_after_parse, {"screen": "screen", "end_early": "end_early"})
    g.add_edge("screen", END)
    g.add_edge("end_early", END)
    return g.compile()


parse_screen_graph = build_parse_screen_graph()


def run_parse_screen(
    application_id: str,
    resume_path: str,
    jd: dict[str, Any],
    resume_text: str = "",
) -> dict[str, Any]:
    result = parse_screen_graph.invoke(
        {
            "application_id": application_id,
            "resume_path": resume_path,
            "resume_text": resume_text,
            "jd": jd,
            "needs_human": False,
            "rejected": False,
            "questions": [],
            "error": "",
        }
    )
    needs_human = bool(result.get("needs_human"))
    return {
        "profile": result.get("profile") or {},
        "screen": {} if needs_human else (result.get("screen") or {}),
        "questions": [],
        "needs_human": needs_human,
        "rejected": False if needs_human else bool(result.get("rejected")),
        "error": result.get("error") or "",
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
    """Legacy alias: questions are generated after interview confirm."""
    return run_parse_screen(application_id, resume_path, jd, resume_text)
