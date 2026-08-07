from __future__ import annotations

from dataclasses import asdict, dataclass, field
from enum import Enum
from typing import Any, Optional


class ApplicationStatus(str, Enum):
    uploaded = "uploaded"
    parsing = "parsing"
    screened = "screened"
    rejected = "rejected"
    questions_ready = "questions_ready"
    scheduled = "scheduled"
    awaiting_reply = "awaiting_reply"
    confirmed = "confirmed"
    declined = "declined"
    needs_human = "needs_human"
    failed = "failed"


@dataclass
class Education:
    school: str = ""
    degree: str = ""
    major: str = ""
    end_year: Optional[int] = None


@dataclass
class Experience:
    company: str = ""
    title: str = ""
    years: float = 0
    highlights: list[str] = field(default_factory=list)


@dataclass
class CandidateProfile:
    name: str = ""
    email: str = ""
    phone: str = ""
    education: list[Education] = field(default_factory=list)
    experiences: list[Experience] = field(default_factory=list)
    skills: list[str] = field(default_factory=list)
    papers: list[str] = field(default_factory=list)
    total_years: float = 0
    raw_text_excerpt: str = ""
    parse_confidence: float = 0.5
    issues: list[str] = field(default_factory=list)

    def model_dump(self) -> dict[str, Any]:
        return asdict(self)

    def model_dump_json(self, indent: int | None = None) -> str:
        import json

        return json.dumps(self.model_dump(), ensure_ascii=False, indent=indent)

    @classmethod
    def model_validate(cls, data: dict[str, Any]) -> "CandidateProfile":
        edu = [Education(**e) if isinstance(e, dict) else e for e in data.get("education") or []]
        exp = [Experience(**e) if isinstance(e, dict) else e for e in data.get("experiences") or []]
        return cls(
            name=data.get("name", ""),
            email=data.get("email", ""),
            phone=data.get("phone", ""),
            education=edu,
            experiences=exp,
            skills=list(data.get("skills") or []),
            papers=list(data.get("papers") or []),
            total_years=float(data.get("total_years") or 0),
            raw_text_excerpt=data.get("raw_text_excerpt", ""),
            parse_confidence=float(data.get("parse_confidence") or 0.5),
            issues=list(data.get("issues") or []),
        )


@dataclass
class ScoreBreakdown:
    education: float = 0
    major: float = 0
    years: float = 0
    skills: float = 0
    projects: float = 0
    papers: float = 0
    weighted_total: float = 0
    hard_fail_reasons: list[str] = field(default_factory=list)
    evidence: list[str] = field(default_factory=list)

    def model_dump(self) -> dict[str, Any]:
        return asdict(self)

    @classmethod
    def model_validate(cls, data: dict[str, Any]) -> "ScoreBreakdown":
        return cls(
            education=float(data.get("education") or 0),
            major=float(data.get("major") or 0),
            years=float(data.get("years") or 0),
            skills=float(data.get("skills") or 0),
            projects=float(data.get("projects") or 0),
            papers=float(data.get("papers") or 0),
            weighted_total=float(data.get("weighted_total") or 0),
            hard_fail_reasons=list(data.get("hard_fail_reasons") or []),
            evidence=list(data.get("evidence") or []),
        )


@dataclass
class InterviewQuestion:
    category: str
    question: str
    difficulty: str = "medium"
    reference_answer: str = ""
    scoring_points: list[str] = field(default_factory=list)
    estimated_minutes: int = 15

    def model_dump(self) -> dict[str, Any]:
        return asdict(self)


class ReplyIntentType(str, Enum):
    accept = "accept"
    decline = "decline"
    reschedule = "reschedule"
    unclear = "unclear"


@dataclass
class ReplyIntent:
    intent: ReplyIntentType
    confidence: float = 0.0
    preferred_windows: list[str] = field(default_factory=list)
    selected_slot_index: Optional[int] = None
    rationale: str = ""

    def model_dump(self) -> dict[str, Any]:
        d = asdict(self)
        d["intent"] = self.intent.value if isinstance(self.intent, ReplyIntentType) else self.intent
        return d

    @classmethod
    def model_validate(cls, data: dict[str, Any]) -> "ReplyIntent":
        raw = data.get("intent", "unclear")
        if isinstance(raw, ReplyIntentType):
            intent = raw
        else:
            try:
                intent = ReplyIntentType(str(raw))
            except ValueError:
                intent = ReplyIntentType.unclear
        idx = data.get("selected_slot_index")
        if idx is not None:
            idx = int(idx)
        return cls(
            intent=intent,
            confidence=float(data.get("confidence") or 0),
            preferred_windows=list(data.get("preferred_windows") or []),
            selected_slot_index=idx,
            rationale=str(data.get("rationale") or ""),
        )


@dataclass
class AuditEvent:
    application_id: str
    action: str
    actor: str = "system"
    before_status: Optional[str] = None
    after_status: Optional[str] = None
    detail: dict[str, Any] = field(default_factory=dict)
    idempotency_key: Optional[str] = None
    langsmith_run_id: Optional[str] = None
