from __future__ import annotations

from hr_agent.agents.parse_heuristics import heuristic_profile
from hr_agent.config.settings import settings
from hr_agent.state.models import CandidateProfile
from hr_agent.tools import llm
from hr_agent.tools.pii import redact


def _norm_list(items: list[str]) -> set[str]:
    return {str(x).strip().lower() for x in items if str(x).strip()}


def _field_agreement(primary: CandidateProfile, secondary: CandidateProfile) -> tuple[float, list[str]]:
    """Return agreement ratio 0-1 and mismatched field labels."""
    checks: list[tuple[str, bool]] = []
    checks.append(("name", (primary.name or "").strip() == (secondary.name or "").strip() or not secondary.name))
    checks.append(
        (
            "total_years",
            abs(float(primary.total_years or 0) - float(secondary.total_years or 0)) <= 1.0,
        )
    )
    p_sk = _norm_list(primary.skills)
    s_sk = _norm_list(secondary.skills)
    if p_sk or s_sk:
        overlap = len(p_sk & s_sk) / max(len(p_sk | s_sk), 1)
        checks.append(("skills", overlap >= 0.5))
    p_companies = {(e.company or "").strip().lower() for e in primary.experiences if e.company}
    s_companies = {(e.company or "").strip().lower() for e in secondary.experiences if e.company}
    if p_companies and s_companies:
        checks.append(("experiences", bool(p_companies & s_companies)))
    mism = [name for name, ok in checks if not ok]
    ratio = sum(1 for _, ok in checks if ok) / max(len(checks), 1)
    return ratio, mism


def verify_parse_profile(raw: str, primary: CandidateProfile) -> tuple[bool, str, dict]:
    """
    Returns (needs_human, reason_code, detail).
    When verify disabled, returns (False, "", {}).
    """
    if not settings.parse_verify_enabled:
        return False, "", {}

    mode = (settings.parse_verify_mode or "dual_llm").lower()
    threshold = settings.parse_verify_field_diff_threshold

    if mode == "rules":
        seed = heuristic_profile(raw)
        _, mism = _field_agreement(primary, seed)
        if mism and len(mism) >= 2:
            return True, "parse_inconsistent", {"mode": "rules", "mismatch": mism}
        return False, "", {"mode": "rules", "ok": True}

    # dual_llm: second vendor re-extracts key fields
    ep = settings.parse_verify
    if not ep.enabled() or not llm.has_llm():
        return False, "", {"mode": "dual_llm", "skipped": "verify_endpoint_unconfigured"}

    system = (
        "你是简历字段校验助手。仅根据简历文本抽取结构化 JSON（CandidateProfile 子集）："
        "name, total_years, skills[], experiences[{company,title,years}]。"
        "不要 Markdown，不要解释。"
    )
    try:
        secondary = llm.structured_invoke(
            ep.model,
            system,
            redact(raw[:12000]),
            CandidateProfile,
        )
    except Exception as exc:
        return False, "", {"mode": "dual_llm", "skipped": str(exc)[:200]}

    ratio, mism = _field_agreement(primary, secondary)
    detail = {
        "mode": "dual_llm",
        "agreement": round(ratio, 3),
        "mismatch": mism,
        "verify_vendor": ep.vendor,
        "verify_model": ep.model,
    }
    if ratio < threshold:
        return True, "parse_cross_vendor_mismatch", detail
    return False, "", detail
