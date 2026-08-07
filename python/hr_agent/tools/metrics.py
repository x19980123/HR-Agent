from __future__ import annotations

import threading
import time
from collections import defaultdict


class Metrics:
    """In-process business metrics (Prometheus-ready shape)."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.counters: dict[str, float] = defaultdict(float)
        self.timings: dict[str, list[float]] = defaultdict(list)

    def inc(self, name: str, value: float = 1.0) -> None:
        with self._lock:
            self.counters[name] += value

    def observe(self, name: str, seconds: float) -> None:
        with self._lock:
            self.timings[name].append(seconds)
            if len(self.timings[name]) > 500:
                self.timings[name] = self.timings[name][-200:]

    def snapshot(self) -> dict:
        with self._lock:
            timing_summary = {}
            for k, vals in self.timings.items():
                if not vals:
                    continue
                timing_summary[k] = {
                    "count": len(vals),
                    "avg_ms": round(1000 * sum(vals) / len(vals), 2),
                    "p95_ms": round(1000 * sorted(vals)[max(0, int(len(vals) * 0.95) - 1)], 2),
                }
            return {"counters": dict(self.counters), "timings": timing_summary, "ts": time.time()}


metrics = Metrics()
