from __future__ import annotations

import json
import re
import urllib.error
import urllib.request
from functools import lru_cache
from typing import Any, Type, TypeVar

from hr_agent.config.settings import LLMEndpoint, settings

T = TypeVar("T")


def _normalize_base(base: str) -> str:
    b = (base or "https://api.openai.com/v1").rstrip("/")
    if not b.endswith("/v1"):
        b = b + "/v1"
    return b


def endpoint_for_model(model_name: str) -> LLMEndpoint:
    """Map a model name back to its step endpoint (key/base may differ per step)."""
    for ep in (
        settings.parse,
        settings.screen,
        settings.question,
        settings.classify,
        settings.parse_verify,
        settings.scheduling,
        settings.scheduling_verify,
    ):
        if ep.model == model_name:
            return ep
    return LLMEndpoint(settings.llm_api_key, settings.llm_api_base, model_name or settings.llm_default_model)


@lru_cache(maxsize=16)
def _chat_model(model_name: str, api_key: str, api_base: str):
    if not api_key:
        return None
    try:
        from langchain_openai import ChatOpenAI
    except ImportError:
        return None
    return ChatOpenAI(
        model=model_name,
        api_key=api_key,
        temperature=0.1,
        base_url=_normalize_base(api_base),
    )


def has_llm() -> bool:
    if settings.offline_mode or not settings.llm_api_key:
        return False
    return True


def llm_backend() -> str:
    if not has_llm():
        return "heuristic"
    ep = settings.parse
    if _chat_model(ep.model, ep.api_key, ep.api_base) is not None:
        return "langchain"
    return "openai_http"


def _http_chat(ep: LLMEndpoint, system: str, user: str) -> str:
    url = _normalize_base(ep.api_base) + "/chat/completions"
    payload = {
        "model": ep.model,
        "temperature": 0.1,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
    }
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
        with urllib.request.urlopen(req, timeout=90) as resp:
            body = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        err = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"LLM HTTP {e.code}: {err[:400]}") from e
    return str(body["choices"][0]["message"]["content"])


def _extract_json(text: str) -> Any:
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*", "", text)
        text = re.sub(r"\s*```$", "", text)
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}|\[[\s\S]*\]", text)
        if not m:
            raise
        return json.loads(m.group(0))


def _to_schema(schema: Type[T], data: Any) -> T:
    if hasattr(schema, "model_validate"):
        return schema.model_validate(data)  # type: ignore[attr-defined]
    return schema(**data)  # type: ignore[misc]


def structured_invoke(model_name: str, system: str, user: str, schema: Type[T]) -> T:
    if not has_llm():
        raise RuntimeError("LLM unavailable")
    ep = endpoint_for_model(model_name)
    if not ep.api_key:
        raise RuntimeError("LLM API key missing for model " + model_name)

    lc = _chat_model(ep.model, ep.api_key, ep.api_base)
    if lc is not None:
        try:
            structured = lc.with_structured_output(schema)
            from langchain_core.messages import HumanMessage, SystemMessage

            return structured.invoke(
                [SystemMessage(content=system), HumanMessage(content=user)]
            )
        except Exception:
            pass

    schema_name = getattr(schema, "__name__", "Result")
    hint = ""
    if hasattr(schema, "__dataclass_fields__"):
        hint = "字段: " + ", ".join(schema.__dataclass_fields__.keys())  # type: ignore[attr-defined]
    sys2 = (
        system
        + f"\n\n只输出一个合法 JSON 对象，对应结构 {schema_name}。"
        + (f" {hint}。" if hint else "")
        + "不要 Markdown，不要解释。"
    )
    content = _http_chat(ep, sys2, user)
    data = _extract_json(content)
    return _to_schema(schema, data)


def freeform_invoke(model_name: str, system: str, user: str) -> str:
    if not has_llm():
        raise RuntimeError("LLM unavailable")
    ep = endpoint_for_model(model_name)
    lc = _chat_model(ep.model, ep.api_key, ep.api_base)
    if lc is not None:
        from langchain_core.messages import HumanMessage, SystemMessage

        msg = lc.invoke([SystemMessage(content=system), HumanMessage(content=user)])
        return str(msg.content)
    return _http_chat(ep, system, user)


def dumps(obj: Any) -> str:
    from dataclasses import asdict, is_dataclass

    if hasattr(obj, "model_dump"):
        return json.dumps(obj.model_dump(), ensure_ascii=False, indent=2)
    if is_dataclass(obj):
        return json.dumps(asdict(obj), ensure_ascii=False, indent=2)
    return json.dumps(obj, ensure_ascii=False, indent=2)
