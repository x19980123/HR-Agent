from __future__ import annotations

import re
from typing import Any

from hr_agent.config.settings import settings
from hr_agent.state.models import ReplyIntent, ReplyIntentType
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


def _heuristic_classify(email_body: str, context: dict[str, Any] | None = None) -> ReplyIntent:
    text = email_body.strip()
    low = text.lower()

    # select slot index: "1" / "选2" / "第二个"
    m = re.search(r"(?:选(?:择)?|回复)?\s*([1-3])\b", text)
    if not m:
        m = re.search(r"第\s*([一二三1-3])\s*个", text)
    selected = None
    if m:
        token = m.group(1)
        mapping = {"一": 0, "二": 1, "三": 2, "1": 0, "2": 1, "3": 2}
        selected = mapping.get(token, None)

    if any(k in text for k in ["接受", "确认", "可以", "没问题", "同意"]) or "accept" in low:
        return ReplyIntent(
            intent=ReplyIntentType.accept,
            confidence=0.9,
            selected_slot_index=selected if selected is not None else 0,
            rationale="keyword accept",
        )
    if any(k in text for k in ["拒绝", "不去", "取消", "不参加"]) or "decline" in low or "reject" in low:
        return ReplyIntent(intent=ReplyIntentType.decline, confidence=0.9, rationale="keyword decline")
    if any(k in text for k in ["改期", "换时间", "另一天", "下周", "延后"]) or "reschedule" in low:
        windows = []
        if "下周" in text:
            windows.append("next_week")
        if "下午" in text:
            windows.append("afternoon")
        if "上午" in text:
            windows.append("morning")
        return ReplyIntent(
            intent=ReplyIntentType.reschedule,
            confidence=0.85,
            preferred_windows=windows,
            rationale="keyword reschedule",
        )
    if selected is not None:
        return ReplyIntent(
            intent=ReplyIntentType.accept,
            confidence=0.8,
            selected_slot_index=selected,
            rationale="selected slot index",
        )
    return ReplyIntent(intent=ReplyIntentType.unclear, confidence=0.4, rationale="no clear intent")


@retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=0.5, min=0.5, max=3))
def classify_reply(email_body: str, context: dict[str, Any] | None = None) -> ReplyIntent:
    if not llm.has_llm():
        return _heuristic_classify(email_body, context)
    system = (
        "你是邮件意图分类器。判断求职者对面试安排的回复："
        "accept / decline / reschedule / unclear。"
        "若选择了第 N 个时段，填充 selected_slot_index（从 0 开始）。"
        "从正文提取 preferred_windows。confidence 表示把握。"
    )
    user = f"Context:\n{llm.dumps(context or {})}\n\nEmail:\n{email_body}"
    return llm.structured_invoke(settings.classify_model, system, user, ReplyIntent)
