from __future__ import annotations

from typing import Any

from hr_agent.config.settings import settings
from hr_agent.state.models import CandidateProfile, ScoreBreakdown
from hr_agent.tools import llm

try:
    from tenacity import retry, stop_after_attempt, wait_exponential
except ImportError:  # pragma: no cover
    def retry(*_a, **_k):
        def wrap(fn):
            return fn
        return wrap

    def stop_after_attempt(*_a, **_k):
        return None

    def wait_exponential(*_a, **_k):
        return None


def _heuristic_screen(profile: CandidateProfile, jd: dict[str, Any]) -> ScoreBreakdown:
    req = jd.get("requirements") or {}
    weights = jd.get("weights") or {
        "education": 15, "major": 10, "years": 20, "skills": 35, "projects": 15, "papers": 5
    }
    needed_years = float(req.get("years") or 0)
    needed_skills = [str(s).lower() for s in (req.get("skills") or [])]
    have = {s.lower() for s in profile.skills}

    years_score = 100.0 if profile.total_years >= needed_years else max(0, 100 * profile.total_years / max(needed_years, 1))
    skill_hits = sum(1 for s in needed_skills if any(s in h or h in s for h in have))
    skills_score = 100.0 * skill_hits / max(len(needed_skills), 1) if needed_skills else 60.0
    education = 70.0
    major = 60.0
    projects = 65.0 if profile.experiences else 40.0
    papers = 80.0 if profile.papers else 40.0

    hard = []
    if needed_years and profile.total_years + 0.5 < needed_years:
        hard.append(f"工作年限不足: {profile.total_years}<{needed_years}")
    if needed_skills and skill_hits == 0:
        hard.append("关键技能无匹配")

    total = (
        education * weights.get("education", 15)
        + major * weights.get("major", 10)
        + years_score * weights.get("years", 20)
        + skills_score * weights.get("skills", 35)
        + projects * weights.get("projects", 15)
        + papers * weights.get("papers", 5)
    ) / max(sum(weights.values()), 1)

    evidence = []
    if profile.skills:
        evidence.append("技能: " + ", ".join(profile.skills[:8]))
    evidence.append(f"年限: {profile.total_years}")

    return ScoreBreakdown(
        education=education,
        major=major,
        years=years_score,
        skills=skills_score,
        projects=projects,
        papers=papers,
        weighted_total=round(total, 2),
        hard_fail_reasons=hard,
        evidence=evidence,
    )


@retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=0.5, max=4))
def screen_candidate(profile: CandidateProfile, jd: dict[str, Any]) -> ScoreBreakdown:
    if not llm.has_llm():
        return _heuristic_screen(profile, jd)
    system = (
        "你是招聘筛选助手。根据 JD 与候选人画像打分，输出 ScoreBreakdown。"
        "分项 0-100；hard_fail_reasons 写必须项不满足原因；evidence 引用简历要点。"
    )
    user = f"JD:\n{llm.dumps(jd)}\n\nProfile:\n{profile.model_dump_json()}"
    return llm.structured_invoke(settings.screen_model, system, user, ScoreBreakdown)
