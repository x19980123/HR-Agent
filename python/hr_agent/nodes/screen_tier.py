from __future__ import annotations

from hr_agent.config.settings import settings
from hr_agent.state.models import ScoreBreakdown


def classify_screen(screen: ScoreBreakdown) -> tuple[str, bool, bool, str]:
    """
    Returns (tier, needs_human, rejected, human_reason_code).
    """
    total = float(screen.weighted_total or 0)
    hard = list(screen.hard_fail_reasons or [])
    reject_max = settings.screen_tier_reject_max
    pass_min = settings.screen_tier_pass_min

    if total < reject_max:
        if hard and total >= reject_max * 0.5:
            return "human_review", True, False, "screen_gray_zone"
        return "auto_reject", False, True, ""

    if total < pass_min:
        return "human_review", True, False, "screen_gray_zone"

    if hard:
        return "human_review", True, False, "screen_hard_fail_review"

    return "auto_pass", False, False, ""
