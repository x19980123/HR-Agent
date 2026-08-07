from __future__ import annotations

from hr_agent.agents.parse_heuristics import heuristic_profile, validate_profile
from hr_agent.state.models import CandidateProfile
from hr_agent.tools.parse_docs import extract_text, ocr_stub


def run_parse_correct(resume_path: str, resume_text: str = "") -> tuple[str, CandidateProfile]:
    """ReAct when langgraph+langchain available; else heuristic with tool-like retry."""
    try:
        from hr_agent.agents.parse_react_langgraph import run_parse_correct as _run

        return _run(resume_path, resume_text)
    except ImportError:
        raw = extract_text(resume_path, resume_text)
        profile = heuristic_profile(raw)
        if "empty_text" in profile.issues or profile.parse_confidence < 0.45:
            raw2 = extract_text(resume_path, "")
            if raw2.strip():
                raw = raw2
                profile = heuristic_profile(raw)
            else:
                _ = ocr_stub(resume_path)
        profile.issues = validate_profile(profile)
        if raw.strip() and profile.total_years > 0 and profile.skills:
            profile.parse_confidence = max(profile.parse_confidence, 0.75)
            profile.issues = [
                i
                for i in profile.issues
                if i not in {"low_confidence", "years_unclear", "no_years", "no_skills"}
            ]
        return raw, profile
