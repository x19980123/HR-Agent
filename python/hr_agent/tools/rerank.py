from __future__ import annotations

import os
from typing import Any

from hr_agent.config.settings import settings


def rerank(query: str, documents: list[str], top_n: int | None = None) -> list[int]:
    """Return indices into `documents` sorted by relevance (best first).

    On failure, returns original order truncated to top_n.
    """
    if not documents:
        return []
    n = top_n if top_n is not None else settings.rag_rerank_top_n
    n = max(1, min(n, len(documents)))
    if not settings.rerank_enabled or not settings.rerank_model:
        return list(range(n))

    key = settings.dashscope_api_key or settings.embedding.api_key
    if not key:
        return list(range(n))

    try:
        import dashscope
        from dashscope import TextReRank
        from http import HTTPStatus
    except Exception:
        return list(range(n))

    os.environ.setdefault("DASHSCOPE_API_KEY", key)
    dashscope.api_key = key
    try:
        resp = TextReRank.call(
            model=settings.rerank_model,
            query=query,
            documents=documents,
            top_n=n,
            return_documents=False,
        )
    except Exception:
        return list(range(n))

    status = getattr(resp, "status_code", None)
    if status is not None and status != HTTPStatus.OK:
        return list(range(n))

    output = getattr(resp, "output", None) or {}
    results = []
    if isinstance(output, dict):
        results = output.get("results") or []
    else:
        results = getattr(output, "results", None) or []

    idxs: list[int] = []
    for r in results:
        if isinstance(r, dict):
            idx = r.get("index")
        else:
            idx = getattr(r, "index", None)
        if idx is None:
            continue
        try:
            i = int(idx)
        except (TypeError, ValueError):
            continue
        if 0 <= i < len(documents) and i not in idxs:
            idxs.append(i)
    if not idxs:
        return list(range(n))
    return idxs[:n]


def rerank_docs(query: str, docs: list[dict[str, Any]], top_n: int | None = None) -> list[dict[str, Any]]:
    texts = [str(d.get("text") or d.get("content") or "") for d in docs]
    order = rerank(query, texts, top_n=top_n)
    return [docs[i] for i in order if 0 <= i < len(docs)]
