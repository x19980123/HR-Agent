from __future__ import annotations

from typing import Any

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


def _cand_by_id(candidates: list[dict]) -> dict[str, dict]:
    out: dict[str, dict] = {}
    for c in candidates or []:
        oid = str(c.get("open_id") or "").strip()
        if oid:
            out[oid] = c
    return out


def _role_ok(cand: dict, role: str) -> bool:
    kinds = [_norm(x) for x in _as_list(cand.get("role_kinds"))]
    return _norm(role) in kinds


def _specs_ok(need: list[str], cand: dict) -> bool:
    if not need:
        return True
    have = {_norm(x) for x in _as_list(cand.get("specialties"))}
    return any(_norm(n) in have for n in need)


def verify_assignment(
    requirements: list[dict],
    by_role: list[dict],
    assigned_open_ids: list[str],
    candidates: list[dict],
    jd_department: str = "",
) -> tuple[bool, str, dict]:
    """
    Returns (needs_human, reason_code, detail).
    When SCHEDULING_VERIFY_ENABLED is false, still runs lightweight rules (hard invariants).
    """
    detail: dict[str, Any] = {"mode": "rules"}
    cands = _cand_by_id(candidates)
    assigned = [str(x).strip() for x in (assigned_open_ids or []) if str(x).strip()]

    # Always: no duplicates
    if len(assigned) != len(set(assigned)):
        return True, "scheduling_duplicate_assignee", {**detail, "assigned": assigned}

    # Build expected headcount from requirements
    expected = 0
    for req in requirements or []:
        hc = int(req.get("headcount") or 1)
        if hc <= 0:
            hc = 1
        expected += hc

    if len(assigned) < expected:
        return True, "scheduling_headcount_short", {
            **detail,
            "expected": expected,
            "got": len(assigned),
        }

    # Per-role checks via by_role (preferred) or reconstruct from requirements order
    role_rows = by_role or []
    if not role_rows and requirements:
        # fall back: cannot verify role membership without by_role
        role_rows = []

    seen: set[str] = set()
    for i, req in enumerate(requirements or []):
        role = str(req.get("role_kind") or "tech")
        hc = int(req.get("headcount") or 1)
        if hc <= 0:
            hc = 1
        match_dept = bool(req.get("match_jd_department"))
        need_specs = _as_list(req.get("specialties"))
        picks: list[str] = []
        if i < len(role_rows):
            picks = _as_list(role_rows[i].get("open_ids"))
        if len(picks) < hc:
            return True, "scheduling_role_unfilled", {
                **detail,
                "role_kind": role,
                "headcount": hc,
                "open_ids": picks,
            }
        for oid in picks[:hc]:
            if oid in seen:
                return True, "scheduling_duplicate_assignee", {**detail, "open_id": oid}
            seen.add(oid)
            cand = cands.get(oid)
            if not cand:
                # fixed open_id without profile: allow if present in assigned
                continue
            if not _role_ok(cand, role):
                return True, "scheduling_role_mismatch", {
                    **detail,
                    "open_id": oid,
                    "role_kind": role,
                    "role_kinds": cand.get("role_kinds"),
                }
            if match_dept and jd_department:
                if _norm(str(cand.get("department") or "")) != _norm(jd_department):
                    return True, "scheduling_department_mismatch", {
                        **detail,
                        "open_id": oid,
                        "department": cand.get("department"),
                        "jd_department": jd_department,
                    }
            if not _specs_ok(need_specs, cand):
                return True, "scheduling_specialties_mismatch", {
                    **detail,
                    "open_id": oid,
                    "need": need_specs,
                    "have": cand.get("specialties"),
                }

    detail["ok"] = True
    detail["checked_roles"] = len(requirements or [])

    if not settings.scheduling_verify_enabled:
        return False, "", detail

    mode = (settings.scheduling_verify_mode or "rules").lower()
    if mode == "rules":
        return False, "", detail

    # dual_llm: second model scores consistency (optional; skip on failure like parse_verify)
    ep = settings.scheduling_verify
    if not ep.enabled() or not llm.has_llm():
        detail["mode"] = "dual_llm"
        detail["skipped"] = "verify_endpoint_unconfigured"
        return False, "", detail

    payload = {
        "jd_department": jd_department,
        "requirements": requirements,
        "by_role": by_role,
        "assigned_open_ids": assigned,
        "candidates": [
            {
                "open_id": c.get("open_id"),
                "name": c.get("name"),
                "department": c.get("department"),
                "role_kinds": c.get("role_kinds"),
                "specialties": c.get("specialties"),
            }
            for c in (candidates or [])[:40]
        ],
    }
    system = (
        "你是面试官指派校验助手。根据 requirements（角色×人数×部门/特长）与 assigned/by_role，"
        "判断指派是否合理。仅输出 JSON：{\"ok\": bool, \"score\": 0-1, \"issues\": [string]}。"
        "不要 Markdown。"
    )
    try:
        from pydantic import BaseModel, Field

        class _V(BaseModel):
            ok: bool = True
            score: float = 1.0
            issues: list[str] = Field(default_factory=list)

        secondary = llm.structured_invoke(ep.model, system, str(payload)[:12000], _V)
        score = float(getattr(secondary, "score", 1.0) or 0)
        ok = bool(getattr(secondary, "ok", True))
        issues = list(getattr(secondary, "issues", []) or [])
        detail.update(
            {
                "mode": "dual_llm",
                "score": round(score, 3),
                "ok": ok,
                "issues": issues,
                "verify_vendor": ep.vendor,
                "verify_model": ep.model,
            }
        )
        threshold = settings.scheduling_verify_score_threshold
        if (not ok) or score < threshold:
            return True, "scheduling_cross_vendor_mismatch", detail
        return False, "", detail
    except Exception as exc:  # noqa: BLE001
        detail["mode"] = "dual_llm"
        detail["skipped"] = str(exc)[:200]
        return False, "", detail
