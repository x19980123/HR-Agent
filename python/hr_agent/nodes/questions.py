from __future__ import annotations

from typing import Any

from hr_agent.config.settings import settings
from hr_agent.state.models import CandidateProfile, InterviewQuestion
from hr_agent.tools import llm
from hr_agent.tools.rag import store

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


def _from_hints(bank_hints: list[dict[str, Any]]) -> list[InterviewQuestion]:
    out: list[InterviewQuestion] = []
    for h in bank_hints:
        qtext = str(h.get("question") or h.get("content") or "").strip()
        if not qtext:
            continue
        ref = str(h.get("reference_answer") or "").strip()
        pts = h.get("scoring_points") or []
        if not isinstance(pts, list):
            pts = []
        if not ref:
            ref = "请结合候选人经历展开追问，并核对关键知识点。"
        if not pts:
            pts = ["概念正确", "能结合项目", "思路清晰"]
        out.append(
            InterviewQuestion(
                category=str(h.get("category") or "fundamentals"),
                difficulty=str(h.get("difficulty") or "medium"),
                question=qtext,
                reference_answer=ref,
                scoring_points=[str(p) for p in pts if str(p).strip()],
                estimated_minutes=15,
            )
        )
    return out[:6]


def _from_rag(profile: CandidateProfile, jd: dict[str, Any]) -> list[InterviewQuestion]:
    query = " ".join(
        [
            str(jd.get("title", "")),
            " ".join(profile.skills),
            " ".join(str(s) for s in (jd.get("requirements") or {}).get("skills") or []),
        ]
    )
    docs = store.retrieve(query, k=4)
    out: list[InterviewQuestion] = []
    for d in docs:
        out.append(
            InterviewQuestion(
                category=d.get("category", "fundamentals"),
                difficulty="medium",
                question=d["text"],
                reference_answer="请结合候选人背景展开追问，覆盖原理、取舍与落地经验。",
                scoring_points=["概念正确", "能结合项目", "思路清晰"],
                estimated_minutes=15,
            )
        )
    cats = {q.category for q in out}
    if "algorithm" not in cats:
        out.append(
            InterviewQuestion(
                category="algorithm",
                question="实现一个线程安全的 LRU Cache，说明复杂度。",
                reference_answer="HashMap + 双向链表；get/put O(1)。",
                scoring_points=["数据结构", "复杂度", "并发"],
            )
        )
    return out[:5]


@retry(stop=stop_after_attempt(2), wait=wait_exponential(multiplier=0.5, min=0.5, max=3))
def generate_questions(
    profile: CandidateProfile,
    jd: dict[str, Any],
    bank_hints: list[dict[str, Any]] | None = None,
) -> list[InterviewQuestion]:
    hints = bank_hints or []
    seed = _from_hints(hints) if hints else _from_rag(profile, jd)
    if not llm.has_llm():
        return _ensure_reference_answers(seed)

    from dataclasses import dataclass, field

    @dataclass
    class QuestionBundle:
        questions: list[dict] = field(default_factory=list)

        @classmethod
        def model_validate(cls, data: dict[str, Any]) -> "QuestionBundle":
            return cls(questions=list(data.get("questions") or []))

    system = (
        "你是面试题辅助助手。基于 JD、候选人画像与题库线索，生成个性化面试题。"
        "覆盖 algorithm / system_design / fundamentals；每题必须含非空 reference_answer 与 scoring_points。"
        "输出 JSON：{\"questions\":[{\"category\":\"...\",\"difficulty\":\"medium\","
        "\"question\":\"...\",\"reference_answer\":\"...\",\"scoring_points\":[\"...\"],"
        "\"estimated_minutes\":15}]}"
    )
    user = (
        f"JD:\n{llm.dumps(jd)}\n\nProfile:\n{profile.model_dump_json()}\n\n"
        f"题库线索（含参考答案，请个性化题干但保留得分点）:\n{llm.dumps({'items': [q.model_dump() for q in seed]})}"
    )
    try:
        bundle = llm.structured_invoke(settings.question_model, system, user, QuestionBundle)
        qs: list[InterviewQuestion] = []
        for item in bundle.questions:
            if isinstance(item, InterviewQuestion):
                qs.append(item)
            elif isinstance(item, dict):
                qs.append(
                    InterviewQuestion(
                        category=str(item.get("category") or "fundamentals"),
                        question=str(item.get("question") or ""),
                        difficulty=str(item.get("difficulty") or "medium"),
                        reference_answer=str(item.get("reference_answer") or ""),
                        scoring_points=list(item.get("scoring_points") or []),
                        estimated_minutes=int(item.get("estimated_minutes") or 15),
                    )
                )
        qs = [q for q in qs if q.question.strip()]
        qs = _ensure_reference_answers(qs)
        if len(qs) < 3:
            return _ensure_reference_answers(seed)
        return qs[:6]
    except Exception:
        return _ensure_reference_answers(seed)


def _ensure_reference_answers(qs: list[InterviewQuestion]) -> list[InterviewQuestion]:
    out: list[InterviewQuestion] = []
    for q in qs:
        if not str(q.reference_answer or "").strip():
            q.reference_answer = "请结合候选人经历与 JD 要求给出可操作的参考答案要点。"
        if not q.scoring_points:
            q.scoring_points = ["概念正确", "能结合项目", "思路清晰"]
        out.append(q)
    return out
