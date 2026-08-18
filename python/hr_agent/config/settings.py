from __future__ import annotations

import os
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path


def _load_dotenv_file(path: Path) -> None:
    if not path.is_file():
        return
    try:
        text = path.read_text(encoding="utf-8-sig")
    except Exception:
        return
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        key = key.strip()
        val = val.strip().strip('"').strip("'")
        os.environ[key] = val


def _env(key: str, default: str = "") -> str:
    return os.getenv(key, default) or default


def _env_first(*keys: str, default: str = "") -> str:
    for k in keys:
        v = os.getenv(k)
        if v is not None and str(v).strip() != "":
            return str(v).strip()
    return default


def _env_bool(key: str, default: bool = False) -> bool:
    v = os.getenv(key)
    if v is None or str(v).strip() == "":
        return default
    return str(v).strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class LLMEndpoint:
    """One chat/embedding endpoint (key + base + model)."""

    api_key: str
    api_base: str
    model: str
    vendor: str = ""

    def enabled(self) -> bool:
        return bool(self.api_key) and bool(self.model)


def _vendor_chat_endpoint(vendor: str, model_override: str = "") -> LLMEndpoint:
    """Resolve chat endpoint from a vendor id + optional model name."""
    v = (vendor or "").strip().lower()
    if not v:
        return LLMEndpoint("", "", model_override or "", vendor="")

    if v == "deepseek":
        key = _env_first("DEEPSEEK_API_KEY", "LLM_API_KEY", "OPENAI_API_KEY")
        base = _env_first("DEEPSEEK_API_BASE", "LLM_API_BASE", "OPENAI_API_BASE") or "https://api.deepseek.com"
        default_model = _env("DEEPSEEK_CHAT_MODEL", "deepseek-chat")
    elif v in ("dashscope", "aliyun"):
        key = _env_first("DASHSCOPE_API_KEY", "EMBEDDING_API_KEY")
        base = _env_first(
            "DASHSCOPE_CHAT_API_BASE",
            "EMBEDDING_API_BASE",
            default="https://dashscope.aliyuncs.com/compatible-mode/v1",
        )
        default_model = _env("DASHSCOPE_CHAT_MODEL", "qwen-plus")
    elif v == "openai":
        key = _env_first("OPENAI_API_KEY", "LLM_API_KEY")
        base = _env_first("OPENAI_API_BASE", "LLM_API_BASE") or "https://api.openai.com/v1"
        default_model = _env("OPENAI_CHAT_MODEL", "gpt-4o-mini")
    else:
        prefix = v.upper().replace("-", "_")
        key = _env(f"{prefix}_API_KEY")
        base = _env(f"{prefix}_API_BASE")
        default_model = _env(f"{prefix}_CHAT_MODEL")

    model = model_override or default_model
    return LLMEndpoint(key, base, model, vendor=v)


class Settings:
    def __init__(self) -> None:
        # --- Legacy flat LLM (still supported) ---
        self.llm_api_key = _env_first("LLM_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY")
        self.llm_api_base = _env_first("LLM_API_BASE", "OPENAI_API_BASE", "DEEPSEEK_API_BASE")
        self.llm_default_model = _env_first("LLM_DEFAULT_MODEL", default="gpt-4o-mini")
        self.llm_default_vendor = _env("LLM_DEFAULT_VENDOR", "")

        self.openai_api_key = self.llm_api_key
        self.openai_api_base = self.llm_api_base

        # --- Vendor profiles (keys reused by model / step via VENDOR=...) ---
        self.deepseek = _vendor_chat_endpoint("deepseek", _env("DEEPSEEK_CHAT_MODEL", ""))
        self.dashscope_chat = _vendor_chat_endpoint("dashscope", _env("DASHSCOPE_CHAT_MODEL", ""))

        # --- Pipeline steps ---
        self.parse = self._step("PARSE", self.llm_default_model)
        self.screen = self._step("SCREEN", self.llm_default_model)
        self.question = self._step("QUESTION", self.llm_default_model)
        self.classify = self._step("CLASSIFY", self.llm_default_model)

        self.parse_model = self.parse.model
        self.screen_model = self.screen.model
        self.question_model = self.question.model
        self.classify_model = self.classify.model

        # --- 2.0: dual-model parse verify (config ready; code in v2.0-alpha) ---
        self.parse_verify_enabled = _env_bool("PARSE_VERIFY_ENABLED", False)
        self.parse_verify_mode = _env("PARSE_VERIFY_MODE", "dual_llm")  # rules | dual_llm
        verify_vendor = _env_first("PARSE_VERIFY_VENDOR", default="dashscope")
        verify_model = _env("PARSE_VERIFY_MODEL", "")
        self.parse_verify = _vendor_chat_endpoint(verify_vendor, verify_model)
        self.parse_verify_field_diff_threshold = float(
            _env("PARSE_VERIFY_FIELD_DIFF_THRESHOLD", "0.75") or "0.75"
        )

        # --- Scheduling assign + verify (Phase 3) ---
        self.scheduling_agent_enabled = _env_bool("SCHEDULING_AGENT_ENABLED", True)
        sched_vendor = _env_first("SCHEDULING_VENDOR", default=_env("LLM_DEFAULT_VENDOR", "deepseek"))
        sched_model = _env("SCHEDULING_MODEL", "")
        self.scheduling = _vendor_chat_endpoint(sched_vendor, sched_model)
        self.scheduling_llm_refine = _env_bool("SCHEDULING_LLM_REFINE", True)
        self.scheduling_verify_enabled = _env_bool("SCHEDULING_VERIFY_ENABLED", True)
        self.scheduling_verify_mode = _env("SCHEDULING_VERIFY_MODE", "dual_llm")  # rules | dual_llm
        sv_vendor = _env_first("SCHEDULING_VERIFY_VENDOR", default="dashscope")
        sv_model = _env("SCHEDULING_VERIFY_MODEL", "")
        self.scheduling_verify = _vendor_chat_endpoint(sv_vendor, sv_model)
        self.scheduling_verify_score_threshold = float(
            _env("SCHEDULING_VERIFY_SCORE_THRESHOLD", "0.75") or "0.75"
        )

        self.screen_tier_reject_max = float(_env("SCREEN_TIER_REJECT_MAX", "30") or "30")
        self.screen_tier_pass_min = float(_env("SCREEN_TIER_PASS_MIN", "60") or "60")

        # --- Embedding / RAG (DashScope; key shared with chat vendor) ---
        self.dashscope_api_key = _env_first("DASHSCOPE_API_KEY", "EMBEDDING_API_KEY")
        self.embedding_provider = (_env("EMBEDDING_PROVIDER", "dashscope") or "dashscope").lower()
        self.embedding = LLMEndpoint(
            api_key=self.dashscope_api_key or _env_first("LLM_API_KEY", "OPENAI_API_KEY"),
            api_base=_env_first("EMBEDDING_API_BASE", "DASHSCOPE_EMBEDDING_API_BASE", default="https://dashscope.aliyuncs.com/compatible-mode/v1"),
            model=_env("EMBEDDING_MODEL", "qwen3.7-text-embedding"),
            vendor="dashscope",
        )
        self.rerank_enabled = _env_bool("RERANK_ENABLED", True)
        self.rerank_model = _env("RERANK_MODEL", "gte-rerank-v2")
        self.rag_retrieve_k = int(_env("RAG_RETRIEVE_K", "12") or "12")
        self.rag_rerank_top_n = int(_env("RAG_RERANK_TOP_N", "4") or "4")
        self.chroma_path = _env("CHROMA_PATH", "./data/chroma")
        self.rag_chunk_size = int(_env("RAG_CHUNK_SIZE", "400") or "400")
        self.rag_chunk_overlap = int(_env("RAG_CHUNK_OVERLAP", "60") or "60")
        self.rag_collection = _env("RAG_COLLECTION", "interview_bank")

        self.langchain_tracing_v2 = _env_bool("LANGCHAIN_TRACING_V2", False)
        self.langchain_api_key = _env("LANGCHAIN_API_KEY")
        self.langchain_project = _env("LANGCHAIN_PROJECT", "hr-agent")
        self.react_max_steps = int(_env("REACT_MAX_STEPS", "4") or "4")
        self.agent_host = _env("AGENT_HOST", "0.0.0.0")
        self.agent_port = int(_env("AGENT_PORT", "8000") or "8000")
        self.offline_mode = _env_bool("OFFLINE_MODE", True)

    def _step(self, prefix: str, default_model: str) -> LLMEndpoint:
        model = _env_first(f"{prefix}_MODEL", "LLM_DEFAULT_MODEL", default=default_model)
        vendor = _env_first(f"{prefix}_VENDOR", "LLM_DEFAULT_VENDOR")
        if vendor:
            ep = _vendor_chat_endpoint(vendor, model)
            step_key = _env_first(f"{prefix}_API_KEY")
            step_base = _env_first(f"{prefix}_API_BASE")
            if step_key:
                return LLMEndpoint(
                    step_key,
                    step_base or ep.api_base,
                    ep.model or model,
                    vendor=ep.vendor or vendor,
                )
            return ep

        return LLMEndpoint(
            api_key=_env_first(f"{prefix}_API_KEY", "LLM_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY"),
            api_base=_env_first(f"{prefix}_API_BASE", "LLM_API_BASE", "OPENAI_API_BASE", "DEEPSEEK_API_BASE"),
            model=model,
            vendor=self.llm_default_vendor or "legacy",
        )

    def endpoint_for(self, step: str) -> LLMEndpoint:
        return {
            "parse": self.parse,
            "parse_verify": self.parse_verify,
            "screen": self.screen,
            "question": self.question,
            "classify": self.classify,
            "scheduling": self.scheduling,
            "scheduling_verify": self.scheduling_verify,
        }.get(step, LLMEndpoint(self.llm_api_key, self.llm_api_base, self.llm_default_model))

    def llm_config_summary(self) -> dict:
        """Safe summary for /health (no secrets)."""
        def _pub(ep: LLMEndpoint) -> dict:
            return {
                "vendor": ep.vendor or None,
                "model": ep.model,
                "api_base": ep.api_base,
                "configured": ep.enabled(),
            }

        return {
            "default_vendor": self.llm_default_vendor or None,
            "offline_mode": self.offline_mode,
            "steps": {
                "parse": _pub(self.parse),
                "parse_verify": _pub(self.parse_verify),
                "screen": _pub(self.screen),
                "question": _pub(self.question),
                "classify": _pub(self.classify),
                "scheduling": _pub(self.scheduling),
                "scheduling_verify": _pub(self.scheduling_verify),
            },
            "parse_verify_enabled": self.parse_verify_enabled,
            "parse_verify_mode": self.parse_verify_mode,
            "scheduling_agent_enabled": self.scheduling_agent_enabled,
            "scheduling_verify_enabled": self.scheduling_verify_enabled,
            "scheduling_verify_mode": self.scheduling_verify_mode,
            "embedding": {
                "provider": self.embedding_provider,
                "model": self.embedding.model,
                "configured": self.embedding.enabled(),
            },
            "rerank": {
                "enabled": self.rerank_enabled,
                "model": self.rerank_model if self.rerank_enabled else None,
            },
        }


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    root = Path(__file__).resolve().parents[2]  # python/
    repo = root.parent  # HR-Agent/
    for p in (repo / ".env", root / ".env", Path.cwd() / ".env"):
        _load_dotenv_file(p)
    try:
        from dotenv import load_dotenv

        load_dotenv(override=True)
    except Exception:
        pass
    return Settings()


settings = get_settings()
