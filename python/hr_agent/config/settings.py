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

    def enabled(self) -> bool:
        return bool(self.api_key) and bool(self.model)


class Settings:
    def __init__(self) -> None:
        # Default LLM (preferred LLM_* ; OPENAI_* kept as alias)
        self.llm_api_key = _env_first("LLM_API_KEY", "OPENAI_API_KEY")
        self.llm_api_base = _env_first("LLM_API_BASE", "OPENAI_API_BASE")
        self.llm_default_model = _env_first("LLM_DEFAULT_MODEL", "PARSE_MODEL", default="gpt-4o-mini")

        # Backward-compatible aliases used by older code paths
        self.openai_api_key = self.llm_api_key
        self.openai_api_base = self.llm_api_base

        self.parse = self._step("PARSE", self.llm_default_model)
        self.screen = self._step("SCREEN", self.llm_default_model)
        self.question = self._step("QUESTION", self.llm_default_model)
        self.classify = self._step("CLASSIFY", self.llm_default_model)

        self.parse_model = self.parse.model
        self.screen_model = self.screen.model
        self.question_model = self.question.model
        self.classify_model = self.classify.model

        # Embedding / RAG
        self.dashscope_api_key = _env_first("DASHSCOPE_API_KEY", "EMBEDDING_API_KEY")
        self.embedding_provider = (_env("EMBEDDING_PROVIDER", "dashscope") or "dashscope").lower()
        self.embedding = LLMEndpoint(
            api_key=_env_first("DASHSCOPE_API_KEY", "EMBEDDING_API_KEY", "LLM_API_KEY", "OPENAI_API_KEY"),
            api_base=_env_first("EMBEDDING_API_BASE", "LLM_API_BASE", "OPENAI_API_BASE"),
            model=_env("EMBEDDING_MODEL", ""),
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
        return LLMEndpoint(
            api_key=_env_first(f"{prefix}_API_KEY", "LLM_API_KEY", "OPENAI_API_KEY"),
            api_base=_env_first(f"{prefix}_API_BASE", "LLM_API_BASE", "OPENAI_API_BASE"),
            model=_env_first(f"{prefix}_MODEL", "LLM_DEFAULT_MODEL", default=default_model),
        )

    def endpoint_for(self, step: str) -> LLMEndpoint:
        return {
            "parse": self.parse,
            "screen": self.screen,
            "question": self.question,
            "classify": self.classify,
        }.get(step, LLMEndpoint(self.llm_api_key, self.llm_api_base, self.llm_default_model))


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
