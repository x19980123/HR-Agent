from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any

from hr_agent.config.settings import settings


def _api_base(base: str) -> str:
    b = (base or "https://api.openai.com/v1").rstrip("/")
    if not b.endswith("/v1"):
        b = b + "/v1"
    return b


def _dashscope_embed(texts: list[str]) -> list[list[float]] | None:
    key = settings.dashscope_api_key or settings.embedding.api_key
    model = settings.embedding.model
    if not key or not model or not texts:
        return None
    try:
        import dashscope
        from dashscope import TextEmbedding
        from http import HTTPStatus
    except Exception:
        return None

    os.environ.setdefault("DASHSCOPE_API_KEY", key)
    dashscope.api_key = key
    try:
        resp = TextEmbedding.call(model=model, input=texts)
    except Exception:
        return None
    status = getattr(resp, "status_code", None)
    if status is not None and status != HTTPStatus.OK:
        return None
    output = getattr(resp, "output", None) or {}
    if isinstance(output, dict):
        embeddings = output.get("embeddings") or []
    else:
        embeddings = getattr(output, "embeddings", None) or []
    if not embeddings:
        # some SDK versions: resp.output['embeddings'][i]['embedding']
        try:
            body = resp if isinstance(resp, dict) else getattr(resp, "__dict__", {})
            embeddings = (body.get("output") or {}).get("embeddings") or []
        except Exception:
            embeddings = []
    vecs: list[list[float]] = []
    # Preserve input order via text_index when present
    ordered = sorted(
        embeddings,
        key=lambda e: int(e.get("text_index", 0) if isinstance(e, dict) else getattr(e, "text_index", 0)),
    )
    for e in ordered:
        emb = e.get("embedding") if isinstance(e, dict) else getattr(e, "embedding", None)
        if not emb:
            return None
        vecs.append(list(emb))
    if len(vecs) != len(texts):
        return None
    return vecs


def _openai_compat_embed(texts: list[str]) -> list[list[float]] | None:
    ep = settings.embedding
    if not ep.api_key or not ep.model or not texts:
        return None
    url = _api_base(ep.api_base) + "/embeddings"
    payload = {"model": ep.model, "input": texts}
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {ep.api_key}",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body: dict[str, Any] = json.loads(resp.read().decode("utf-8"))
    except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, json.JSONDecodeError, KeyError):
        return None
    items = body.get("data") or []
    items = sorted(items, key=lambda x: int(x.get("index", 0)))
    out = [list(it.get("embedding") or []) for it in items]
    if len(out) != len(texts) or any(not v for v in out):
        return None
    return out


def embed_texts(texts: list[str]) -> list[list[float]] | None:
    """Embed texts via DashScope SDK first, then OpenAI-compatible HTTP."""
    if not texts:
        return None
    provider = settings.embedding_provider
    if provider in {"dashscope", "aliyun", "qwen", ""}:
        vecs = _dashscope_embed(texts)
        if vecs is not None:
            return vecs
    return _openai_compat_embed(texts)


def embedding_backend() -> str:
    if not settings.embedding.model:
        return "none"
    # Probe without network: prefer declared provider
    if settings.embedding_provider in {"dashscope", "aliyun", "qwen", ""} and (
        settings.dashscope_api_key or settings.embedding.api_key
    ):
        return "dashscope"
    if settings.embedding.api_key:
        return "openai_compat"
    return "none"


class CompatibleEmbeddingFunction:
    """Chroma embedding function backed by configured embed provider."""

    def __init__(self) -> None:
        self._model = settings.embedding.model

    def name(self) -> str:
        return f"{embedding_backend()}:{self._model or 'none'}"

    def __call__(self, input: list[str]) -> list[list[float]]:  # noqa: A002
        vecs = embed_texts(list(input))
        if vecs is None:
            raise RuntimeError("embedding unavailable")
        return vecs
