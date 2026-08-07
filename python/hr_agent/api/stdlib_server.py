from __future__ import annotations

"""Minimal stdlib FastAPI-compatible agent server when fastapi is unavailable.

Prefer `hr_agent.api.server:app` when deps installed.
"""

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

from hr_agent.config.settings import settings
from hr_agent.graph.pipeline import run_generate_questions, run_parse_screen
from hr_agent.nodes.classify_reply import classify_reply
from hr_agent.tools import llm
from hr_agent.tools.pii import redact
from hr_agent.tools.rag import store as rag_store


class Handler(BaseHTTPRequestHandler):
    def _json(self, code: int, payload: dict) -> None:
        raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _read_json(self) -> dict:
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n) if n else b"{}"
        return json.loads(body.decode("utf-8") or "{}")

    def do_GET(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/health":
            self._json(
                200,
                {
                    "status": "ok",
                    "server": "stdlib",
                    "llm_backend": llm.llm_backend(),
                    "offline_mode": settings.offline_mode,
                    "model": settings.parse_model,
                    "rag": rag_store.stats(),
                },
            )
            return
        if path == "/v1/rag/stats":
            self._json(200, rag_store.stats())
            return
        self._json(404, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        try:
            req = self._read_json()
            if path in ("/v1/pipeline/parse_screen", "/v1/pipeline/parse_screen_questions"):
                out = run_parse_screen(
                    application_id=req.get("application_id", ""),
                    resume_path=req.get("resume_path", ""),
                    resume_text=req.get("resume_text", ""),
                    jd=req.get("jd") or {},
                )
                out["langsmith_run_id"] = ""
                out["llm_backend"] = llm.llm_backend()
                self._json(200, out)
                return
            if path == "/v1/pipeline/generate_questions":
                out = run_generate_questions(
                    application_id=req.get("application_id", ""),
                    profile=req.get("profile") or {},
                    jd=req.get("jd") or {},
                )
                out["langsmith_run_id"] = ""
                out["llm_backend"] = llm.llm_backend()
                self._json(200, out)
                return
            if path == "/v1/pipeline/classify":
                intent = classify_reply(req.get("email_body", ""), req.get("context") or {})
                data = intent.model_dump()
                data["intent"] = intent.intent.value
                data["langsmith_run_id"] = ""
                data["llm_backend"] = llm.llm_backend()
                self._json(200, data)
                return
            if path == "/v1/rag/upsert":
                self._json(200, rag_store.upsert(req.get("item") or req))
                return
            if path == "/v1/rag/delete":
                self._json(200, rag_store.delete(req.get("id") or ""))
                return
            if path == "/v1/rag/reindex":
                items = req.get("items") or []
                if not isinstance(items, list):
                    items = []
                self._json(200, rag_store.reindex(items))
                return
            if path == "/v1/rag/query":
                docs = rag_store.retrieve(str(req.get("query") or ""), int(req.get("k") or 4))
                self._json(200, {"items": docs})
                return
            self._json(404, {"error": "not found"})
        except Exception as e:  # noqa: BLE001
            self._json(500, {"error": redact(str(e))})

    def log_message(self, fmt: str, *args) -> None:
        print("[agent]", fmt % args)


def main() -> None:
    if settings.langchain_tracing_v2 and settings.langchain_api_key:
        os.environ["LANGCHAIN_TRACING_V2"] = "true"
        os.environ["LANGCHAIN_API_KEY"] = settings.langchain_api_key
        os.environ["LANGCHAIN_PROJECT"] = settings.langchain_project

    host = settings.agent_host
    port = settings.agent_port
    httpd = ThreadingHTTPServer((host, port), Handler)
    print(f"stdlib agent listening on {host}:{port}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()
