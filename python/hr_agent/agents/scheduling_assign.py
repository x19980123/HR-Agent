from __future__ import annotations

from datetime import datetime, timedelta, timezone
from itertools import combinations
from typing import Any

from hr_agent.agents.scheduling_verify import verify_assignment
from hr_agent.config.settings import settings
from hr_agent.tools import llm


def _norm(s: str) -> str:
    return (s or "").strip().lower()


def _as_list(v: Any) -> list[str]:
    if v is None:
        return []
    if isinstance(v, str):
        return [x.strip() for x in v.replace("，", ",").split(",") if x.strip()]
    if isinstance(v, (list, tuple)):
        return [str(x).strip() for x in v if str(x).strip()]
    return []


def _parse_ts(s: str) -> datetime | None:
    if not s:
        return None
    try:
        # support Z
        return datetime.fromisoformat(s.replace("Z", "+00:00"))
    except Exception:
        return None


def _busy_map(busy_intervals: list[dict]) -> dict[str, list[tuple[datetime, datetime]]]:
    out: dict[str, list[tuple[datetime, datetime]]] = {}
    for b in busy_intervals or []:
        oid = str(b.get("open_id") or "").strip()
        start = _parse_ts(str(b.get("starts_at") or b.get("start") or ""))
        end = _parse_ts(str(b.get("ends_at") or b.get("end") or ""))
        if not oid or not start or not end:
            continue
        out.setdefault(oid, []).append((start, end))
    return out


def _overlaps(start: datetime, end: datetime, busy: list[tuple[datetime, datetime]]) -> bool:
    for bs, be in busy:
        if start < be and end > bs:
            return True
    return False


def _count_free_slots(
    open_ids: list[str],
    busy: dict[str, list[tuple[datetime, datetime]]],
    window_start: datetime,
    window_end: datetime,
    duration_min: int,
) -> int:
    """Count weekday 10-18 slots of duration where ALL open_ids are free (panel)."""
    if duration_min <= 0:
        duration_min = 60
    dur = timedelta(minutes=duration_min)
    # normalize tz-aware comparison
    t = window_start
    if t.tzinfo is None:
        t = t.replace(tzinfo=timezone.utc)
    end = window_end
    if end.tzinfo is None:
        end = end.replace(tzinfo=timezone.utc)
    ws = window_start if window_start.tzinfo else window_start.replace(tzinfo=t.tzinfo)
    t = t.replace(hour=10, minute=0, second=0, microsecond=0)
    if t < ws:
        t = t + timedelta(days=1)
    count = 0
    guard = 0
    while t < end and guard < 400:
        guard += 1
        if t.weekday() >= 5:
            t = (t + timedelta(days=1)).replace(hour=10, minute=0, second=0, microsecond=0)
            continue
        slot_end = t + dur
        if slot_end.hour > 18 or (slot_end.hour == 18 and slot_end.minute > 0):
            t = (t + timedelta(days=1)).replace(hour=10, minute=0, second=0, microsecond=0)
            continue
        ok = True
        for oid in open_ids:
            if _overlaps(t, slot_end, busy.get(oid, [])):
                ok = False
                break
        if ok:
            count += 1
        nxt = t + timedelta(hours=2)
        if nxt.hour >= 18:
            t = (t + timedelta(days=1)).replace(hour=10, minute=0, second=0, microsecond=0)
        else:
            t = nxt
    return count


def _score_candidate(
    cand: dict,
    role: str,
    need_specs: list[str],
    match_dept: bool,
    jd_dept: str,
) -> float:
    score = 0.0
    kinds = [_norm(x) for x in _as_list(cand.get("role_kinds"))]
    if _norm(role) in kinds:
        score += 3.0
    else:
        return -1.0
    if match_dept and jd_dept:
        if _norm(str(cand.get("department") or "")) == _norm(jd_dept):
            score += 2.0
        else:
            return -1.0
    elif jd_dept and _norm(str(cand.get("department") or "")) == _norm(jd_dept):
        score += 0.5
    if need_specs:
        have = {_norm(x) for x in _as_list(cand.get("specialties"))}
        hits = sum(1 for n in need_specs if _norm(n) in have)
        if hits == 0:
            return -1.0
        score += min(2.0, hits * 1.0)
    if cand.get("enabled") is False:
        return -1.0
    return score


def _eligible_for_role(
    candidates: list[dict],
    role: str,
    need_specs: list[str],
    match_dept: bool,
    jd_dept: str,
    exclude: set[str],
    fixed: list[str],
) -> list[tuple[float, dict]]:
    ranked: list[tuple[float, dict]] = []
    by_id = {str(c.get("open_id") or ""): c for c in candidates}
    for oid in fixed:
        if oid in exclude:
            continue
        c = by_id.get(oid) or {"open_id": oid, "role_kinds": [role], "enabled": True}
        sc = _score_candidate(c, role, need_specs, match_dept, jd_dept)
        if sc < 0 and by_id.get(oid) is None:
            # fixed without profile: accept
            sc = 5.0
        if sc >= 0:
            ranked.append((sc + 10.0, c if by_id.get(oid) else {"open_id": oid, "role_kinds": [role]}))
    for c in candidates:
        oid = str(c.get("open_id") or "").strip()
        if not oid or oid in exclude or oid in fixed:
            continue
        sc = _score_candidate(c, role, need_specs, match_dept, jd_dept)
        if sc >= 0:
            ranked.append((sc, c))
    ranked.sort(key=lambda x: (-x[0], str(x[1].get("open_id"))))
    return ranked


def _deterministic_assign(
    requirements: list[dict],
    candidates: list[dict],
    busy_intervals: list[dict],
    jd_department: str,
    duration_min: int,
    window_start: datetime,
    window_end: datetime,
) -> tuple[list[str], list[dict], str, bool, str]:
    busy = _busy_map(busy_intervals)
    exclude: set[str] = set()
    by_role: list[dict] = []
    all_ids: list[str] = []
    notes: list[str] = []

    for req in requirements or []:
        role = str(req.get("role_kind") or "tech")
        hc = int(req.get("headcount") or 1)
        if hc <= 0:
            hc = 1
        match_dept = bool(req.get("match_jd_department"))
        need_specs = _as_list(req.get("specialties"))
        fixed = _as_list(req.get("fixed_open_ids"))
        ranked = _eligible_for_role(
            candidates, role, need_specs, match_dept, jd_department, exclude, fixed
        )
        pool = [c for _, c in ranked]
        if len(pool) < hc:
            return (
                [],
                by_role,
                f"role {role} needs {hc}, eligible {len(pool)}",
                True,
                "interviewers_unassigned",
            )

        # search combinations for max panel free slots among top candidates
        top = pool[: min(len(pool), max(hc + 4, 8))]
        best_combo: list[str] | None = None
        best_free = -1
        best_score = -1.0
        for combo in combinations([str(c.get("open_id")) for c in top], hc):
            ids = list(combo)
            if any(i in exclude for i in ids):
                continue
            free_n = _count_free_slots(ids, busy, window_start, window_end, duration_min)
            score_sum = 0.0
            for oid in ids:
                for sc, c in ranked:
                    if str(c.get("open_id")) == oid:
                        score_sum += sc
                        break
            key = (free_n, score_sum)
            if free_n > best_free or (free_n == best_free and score_sum > best_score):
                best_free = free_n
                best_score = score_sum
                best_combo = ids

        if not best_combo:
            return (
                [],
                by_role,
                f"role {role}: no combination",
                True,
                "interviewers_unassigned",
            )

        for oid in best_combo:
            exclude.add(oid)
            all_ids.append(oid)
        by_role.append(
            {
                "role_kind": role,
                "headcount": hc,
                "open_ids": best_combo,
                "panel_free_slots": best_free,
                "sources": ["scheduling_agent"],
            }
        )
        notes.append(f"{role}×{hc}→{','.join(best_combo)} (free≈{best_free})")

    rationale = "deterministic: " + "; ".join(notes)
    return all_ids, by_role, rationale, False, ""


def _llm_refine(
    requirements: list[dict],
    candidates: list[dict],
    by_role: list[dict],
    assigned: list[str],
    jd_department: str,
) -> tuple[list[str], list[dict], str] | None:
    ep = settings.scheduling
    if not ep.enabled() or not llm.has_llm() or settings.offline_mode:
        return None
    system = (
        "你是面试排期选人助手。在给定候选与角色需求下，输出 JSON："
        '{"by_role":[{"role_kind":str,"headcount":int,"open_ids":[str]}],'
        '"assigned_open_ids":[str],"rationale":str}。'
        "必须满足每角色人数、open_id 不重复、优先匹配部门与特长。不要 Markdown。"
    )
    user = str(
        {
            "jd_department": jd_department,
            "requirements": requirements,
            "candidates": [
                {
                    "open_id": c.get("open_id"),
                    "name": c.get("name"),
                    "department": c.get("department"),
                    "role_kinds": c.get("role_kinds"),
                    "specialties": c.get("specialties"),
                }
                for c in (candidates or [])[:50]
            ],
            "seed_by_role": by_role,
            "seed_assigned": assigned,
        }
    )[:14000]
    try:
        from pydantic import BaseModel, Field

        class _Role(BaseModel):
            role_kind: str = "tech"
            headcount: int = 1
            open_ids: list[str] = Field(default_factory=list)

        class _Out(BaseModel):
            by_role: list[_Role] = Field(default_factory=list)
            assigned_open_ids: list[str] = Field(default_factory=list)
            rationale: str = ""

        out = llm.structured_invoke(ep.model, system, user, _Out)
        roles = [r.model_dump() for r in (out.by_role or [])]
        ids = list(out.assigned_open_ids or [])
        if not ids and roles:
            for r in roles:
                ids.extend(r.get("open_ids") or [])
        if not ids:
            return None
        return ids, roles, (out.rationale or "llm_refine")
    except Exception:
        return None


def run_scheduling_assign(payload: dict) -> dict:
    """
    Main entry for POST /v1/scheduling/assign.
    """
    requirements = list(payload.get("requirements") or [])
    candidates = list(payload.get("candidates") or [])
    busy_intervals = list(payload.get("busy_intervals") or [])
    jd_department = str(payload.get("jd_department") or "")
    duration_min = int(payload.get("duration_minutes") or 60)

    ws = _parse_ts(str(payload.get("window_start") or ""))
    we = _parse_ts(str(payload.get("window_end") or ""))
    if not ws:
        ws = datetime.now(timezone.utc) + timedelta(hours=24)
    if not we:
        we = ws + timedelta(days=14)

    if not requirements:
        return {
            "assigned_open_ids": [],
            "by_role": [],
            "needs_human": True,
            "human_reason_code": "interview_plan_missing",
            "error": "no requirements",
            "rationale": "",
            "verify_detail": {},
            "assignment_detail": {"resolver": "scheduling_agent"},
        }

    assigned, by_role, rationale, needs, code = _deterministic_assign(
        requirements,
        candidates,
        busy_intervals,
        jd_department,
        duration_min,
        ws,
        we,
    )
    resolver = "scheduling_deterministic"

    if not needs and settings.scheduling_llm_refine:
        refined = _llm_refine(requirements, candidates, by_role, assigned, jd_department)
        if refined:
            assigned, by_role, rationale = refined
            resolver = "scheduling_llm"

    if needs:
        detail = {
            "resolver": resolver,
            "rationale": rationale,
            "by_role": by_role,
        }
        return {
            "assigned_open_ids": assigned,
            "by_role": by_role,
            "needs_human": True,
            "human_reason_code": code or "interviewers_unassigned",
            "error": rationale,
            "rationale": rationale,
            "verify_detail": {},
            "assignment_detail": detail,
        }

    v_needs, v_code, v_detail = verify_assignment(
        requirements, by_role, assigned, candidates, jd_department
    )
    assignment_detail = {
        "resolver": resolver,
        "rationale": rationale,
        "by_role": by_role,
        "verify": v_detail,
    }
    if v_needs:
        return {
            "assigned_open_ids": assigned,
            "by_role": by_role,
            "needs_human": True,
            "human_reason_code": v_code or "scheduling_verify_failed",
            "error": f"scheduling verify failed: {v_code}",
            "rationale": rationale,
            "verify_detail": v_detail,
            "assignment_detail": assignment_detail,
        }

    return {
        "assigned_open_ids": assigned,
        "by_role": by_role,
        "needs_human": False,
        "human_reason_code": "",
        "error": "",
        "rationale": rationale,
        "verify_detail": v_detail,
        "assignment_detail": assignment_detail,
    }
