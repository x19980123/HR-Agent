from __future__ import annotations

import re

from hr_agent.state.models import CandidateProfile, Education, Experience
from hr_agent.tools.pii import redact


def heuristic_profile(raw: str) -> CandidateProfile:
    email_m = re.search(r"[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}", raw)
    phone_m = re.search(r"1[3-9]\d{9}", raw)
    years = 0.0
    ym = re.search(r"(?:工作年限|工作经验)[:：]?\s*(\d+(?:\.\d+)?)\s*年", raw)
    if not ym:
        ym = re.search(r"(\d+(?:\.\d+)?)\s*年", raw)
    if ym:
        years = float(ym.group(1))

    skills: list[str] = []
    skill_line = re.search(r"技能[:：]\s*(.+)", raw)
    if skill_line:
        parts = re.split(r"[,，、/\s]+", skill_line.group(1).strip())
        skills = [p for p in parts if p and p not in {"无", "-"}]
    for kw in ["Go", "Golang", "Python", "Java", "MySQL", "Redis", "Kafka", "Kubernetes", "Docker", "微服务"]:
        if re.search(re.escape(kw), raw, re.I) and kw not in skills:
            skills.append("Go" if kw.lower() == "golang" else kw)

    name = ""
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        if any(k in line for k in ("邮箱", "手机", "电话", "教育", "工作", "技能", "论文", "简历", "@")):
            continue
        if len(line) <= 20:
            name = line
            break

    education = _parse_education(raw)
    experiences = _parse_experiences(raw, years)

    issues: list[str] = []
    conf = 0.75
    if not raw.strip():
        issues.append("empty_text")
        conf = 0.1
    if years <= 0:
        issues.append("years_unclear")
        conf = min(conf, 0.5)
    if not education:
        issues.append("education_missing")
        conf = min(conf, 0.65)
    if not experiences:
        issues.append("experience_missing")
        conf = min(conf, 0.65)

    return CandidateProfile(
        name=name,
        email=email_m.group(0) if email_m else "",
        phone=phone_m.group(0) if phone_m else "",
        education=education,
        experiences=experiences,
        skills=skills,
        total_years=years,
        raw_text_excerpt=redact(raw[:800]),
        parse_confidence=conf,
        issues=issues,
    )


def _parse_education(raw: str) -> list[Education]:
    out: list[Education] = []
    # e.g. 某某大学 计算机科学与技术 本科 2018
    for m in re.finditer(
        r"([^\s\n]{2,30}?(?:大学|学院|学校))\s+([^\s\n]{2,40})\s+(博士|硕士|本科|大专|学士)\s+(\d{4})",
        raw,
    ):
        out.append(
            Education(
                school=m.group(1),
                major=m.group(2),
                degree=m.group(3),
                end_year=int(m.group(4)),
            )
        )
    if out:
        return out
    block = _section(raw, "教育经历", ("工作经历", "技能", "项目经历", "论文"))
    for line in block.splitlines():
        line = line.strip()
        if not line or line.startswith("教育"):
            continue
        parts = re.split(r"\s+", line)
        if len(parts) >= 3:
            year = None
            if re.fullmatch(r"\d{4}", parts[-1]):
                year = int(parts[-1])
                parts = parts[:-1]
            degree = parts[-1] if parts else ""
            major = parts[-2] if len(parts) >= 2 else ""
            school = " ".join(parts[:-2]) if len(parts) >= 3 else (parts[0] if parts else "")
            out.append(Education(school=school, major=major, degree=degree, end_year=year))
            break
    return out


def _parse_experiences(raw: str, fallback_years: float) -> list[Experience]:
    out: list[Experience] = []
    block = _section(raw, "工作经历", ("技能", "教育经历", "论文", "项目经历"))
    lines = [ln.strip() for ln in block.splitlines() if ln.strip() and not ln.strip().startswith("工作")]
    i = 0
    while i < len(lines):
        line = lines[i]
        if line.startswith("-") or line.startswith("•"):
            i += 1
            continue
        # ABC科技 后端工程师 2020-2024
        m = re.match(r"^(\S+)\s+(\S+)\s+(\d{4})\s*[-~—–]\s*(\d{4}|至今|现在)", line)
        company, title, years = "", "", 0.0
        if m:
            company, title = m.group(1), m.group(2)
            try:
                start = int(m.group(3))
                end_s = m.group(4)
                end = 2026 if end_s in {"至今", "现在"} else int(end_s)
                years = max(0.0, float(end - start))
            except ValueError:
                years = fallback_years
        else:
            parts = re.split(r"\s+", line)
            if len(parts) >= 2:
                company, title = parts[0], parts[1]
                years = fallback_years
            else:
                i += 1
                continue
        highlights: list[str] = []
        j = i + 1
        while j < len(lines) and (lines[j].startswith("-") or lines[j].startswith("•")):
            highlights.append(re.sub(r"^[-•]\s*", "", lines[j]))
            j += 1
        out.append(Experience(company=company, title=title, years=years or fallback_years, highlights=highlights))
        i = j
    return out


def _section(raw: str, start: str, ends: tuple[str, ...]) -> str:
    idx = raw.find(start)
    if idx < 0:
        return ""
    rest = raw[idx + len(start) :]
    cut = len(rest)
    for e in ends:
        p = rest.find(e)
        if p >= 0:
            cut = min(cut, p)
    return rest[:cut]


def validate_profile(profile: CandidateProfile) -> list[str]:
    issues = list(profile.issues)
    if profile.parse_confidence < 0.5:
        issues.append("low_confidence")
    if not profile.skills:
        issues.append("no_skills")
    if profile.total_years <= 0:
        issues.append("no_years")
    # strip placeholder junk instead of showing as garbled UI
    placeholders = {"未知", "unknown", "N/A", "-", "—", "n/a"}
    cleaned_edu: list[Education] = []
    for e in profile.education:
        if (e.degree or "").strip() in placeholders:
            e.degree = ""
        if (e.major or "").strip() in placeholders:
            e.major = ""
        if (e.school or "").strip() in placeholders:
            e.school = ""
        if e.school or e.major or e.degree:
            cleaned_edu.append(e)
    profile.education = cleaned_edu
    cleaned_exp: list[Experience] = []
    for e in profile.experiences:
        if (e.company or "").strip() in placeholders:
            e.company = ""
        if (e.title or "").strip() in placeholders:
            e.title = ""
        if e.company or e.title or e.highlights:
            cleaned_exp.append(e)
    profile.experiences = cleaned_exp
    if not profile.education:
        issues.append("education_missing")
    if not profile.experiences:
        issues.append("experience_missing")
    if profile_is_empty(profile):
        issues.append("empty_profile")
    return list(dict.fromkeys(issues))


def profile_is_empty(profile: CandidateProfile) -> bool:
    has_edu = any(e.school or e.major or e.degree for e in profile.education)
    has_exp = any(e.company or e.title or e.highlights for e in profile.experiences)
    return not (has_edu or has_exp or profile.skills)


def needs_human_after_parse(raw: str, profile: CandidateProfile) -> tuple[bool, str]:
    """Force human review when extraction/profile is unusable; never screen these."""
    profile.issues = validate_profile(profile)
    reasons: list[str] = []
    text = (raw or "").strip()
    if not text or text.startswith("[ocr_stub]"):
        reasons.append("简历文本为空且 OCR 未识别到有效内容（请上传更清晰的扫描件，或粘贴文本后重试解析）")
        if "empty_text" not in profile.issues:
            profile.issues.append("empty_text")
        profile.parse_confidence = min(float(profile.parse_confidence or 0), 0.1)
    if "empty_text" in profile.issues and "简历文本为空" not in "".join(reasons):
        reasons.append("未能抽取到有效简历正文")
    if profile_is_empty(profile):
        reasons.append("画像实质为空（无技能/教育/经历），无法自动筛选")
        if "empty_profile" not in profile.issues:
            profile.issues.append("empty_profile")
        profile.parse_confidence = min(float(profile.parse_confidence or 0), 0.3)
    if float(profile.parse_confidence or 0) < 0.45:
        reasons.append(f"解析置信度过低（{float(profile.parse_confidence):.2f}）")
    # unique preserve order
    reasons = list(dict.fromkeys(reasons))
    return bool(reasons), "；".join(reasons)
