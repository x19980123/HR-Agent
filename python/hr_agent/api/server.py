from __future__ import annotations

"""Prefer FastAPI app; fall back to stdlib HTTP server entry."""

from hr_agent.config.settings import settings

try:
    import os

    from fastapi import FastAPI
    from pydantic import BaseModel, Field

    from hr_agent.graph.pipeline import run_generate_questions, run_parse_screen
    from hr_agent.nodes.classify_reply import classify_reply
    from hr_agent.tools.pii import redact
    from hr_agent.tools.rag import store as rag_store

    if settings.langchain_tracing_v2 and settings.langchain_api_key:
        os.environ["LANGCHAIN_TRACING_V2"] = "true"
        os.environ["LANGCHAIN_API_KEY"] = settings.langchain_api_key
        os.environ["LANGCHAIN_PROJECT"] = settings.langchain_project

    app = FastAPI(title="HR Agent Python Service", version="0.2.0")

    class PipelineReq(BaseModel):
        application_id: str
        resume_path: str = ""
        resume_text: str = ""
        jd: dict = Field(default_factory=dict)

    class QuestionsReq(BaseModel):
        application_id: str = ""
        profile: dict = Field(default_factory=dict)
        jd: dict = Field(default_factory=dict)
        bank_hints: list[dict] = Field(default_factory=list)

    class ClassifyReq(BaseModel):
        application_id: str
        email_body: str
        context: dict = Field(default_factory=dict)

    class RagItemReq(BaseModel):
        item: dict = Field(default_factory=dict)

    class RagDeleteReq(BaseModel):
        id: str

    class RagReindexReq(BaseModel):
        items: list[dict] = Field(default_factory=list)

    class RagQueryReq(BaseModel):
        query: str = ""
        k: int = 4

    @app.get("/health")
    def health():
        return {
            "status": "ok",
            "offline_mode": str(settings.offline_mode or not bool(settings.openai_api_key)),
            "llm": settings.llm_config_summary(),
            "rag": rag_store.stats(),
        }

    @app.get("/v1/rag/stats")
    def rag_stats():
        return rag_store.stats()

    @app.post("/v1/rag/upsert")
    def rag_upsert(req: RagItemReq):
        return rag_store.upsert(req.item or {})

    @app.post("/v1/rag/delete")
    def rag_delete(req: RagDeleteReq):
        return rag_store.delete(req.id)

    @app.post("/v1/rag/reindex")
    def rag_reindex(req: RagReindexReq):
        return rag_store.reindex(req.items or [])

    @app.post("/v1/rag/query")
    def rag_query(req: RagQueryReq):
        return {"items": rag_store.retrieve(req.query, req.k)}

    class ExtractContactReq(BaseModel):
        resume_path: str = ""
        resume_text: str = ""

    @app.post("/v1/extract-contact")
    def extract_contact_api(req: ExtractContactReq):
        from hr_agent.agents.contact_extract import extract_contact

        return extract_contact(resume_path=req.resume_path, resume_text=req.resume_text)

    def _parse_screen(req: PipelineReq):
        try:
            out = run_parse_screen(
                application_id=req.application_id,
                resume_path=req.resume_path,
                resume_text=req.resume_text,
                jd=req.jd,
            )
            out["langsmith_run_id"] = os.getenv("LANGSMITH_RUN_ID", "")
            return out
        except Exception as e:  # noqa: BLE001
            return {
                "profile": {},
                "screen": {},
                "questions": [],
                "needs_human": True,
                "rejected": False,
                "error": redact(str(e)),
            }

    @app.post("/v1/pipeline/parse_screen")
    def parse_screen(req: PipelineReq):
        return _parse_screen(req)

    @app.post("/v1/pipeline/parse_screen_questions")
    def parse_screen_questions(req: PipelineReq):
        return _parse_screen(req)

    @app.post("/v1/pipeline/generate_questions")
    def generate_questions_api(req: QuestionsReq):
        try:
            out = run_generate_questions(
                application_id=req.application_id,
                profile=req.profile,
                jd=req.jd,
                bank_hints=req.bank_hints or None,
            )
            out["langsmith_run_id"] = os.getenv("LANGSMITH_RUN_ID", "")
            return out
        except Exception as e:  # noqa: BLE001
            return {"questions": [], "error": redact(str(e))}

    @app.post("/v1/pipeline/classify")
    def classify(req: ClassifyReq):
        try:
            intent = classify_reply(req.email_body, req.context)
            data = intent.model_dump()
            data["intent"] = intent.intent.value
            data["langsmith_run_id"] = ""
            return data
        except Exception as e:  # noqa: BLE001
            return {
                "intent": "unclear",
                "confidence": 0.0,
                "preferred_windows": [],
                "selected_slot_index": None,
                "error": redact(str(e)),
            }

    class SchedulingAssignReq(BaseModel):
        application_id: str = ""
        round_index: int = 0
        jd_round_id: str = ""
        jd_department: str = ""
        duration_minutes: int = 60
        requirements: list[dict] = Field(default_factory=list)
        candidates: list[dict] = Field(default_factory=list)
        busy_intervals: list[dict] = Field(default_factory=list)
        window_start: str = ""
        window_end: str = ""

    @app.post("/v1/scheduling/assign")
    def scheduling_assign_api(req: SchedulingAssignReq):
        from hr_agent.agents.scheduling_assign import run_scheduling_assign

        try:
            return run_scheduling_assign(req.model_dump())
        except Exception as e:  # noqa: BLE001
            return {
                "assigned_open_ids": [],
                "by_role": [],
                "needs_human": True,
                "human_reason_code": "scheduling_agent_error",
                "error": redact(str(e)),
                "rationale": "",
                "verify_detail": {},
                "assignment_detail": {"resolver": "scheduling_agent", "error": redact(str(e))},
            }

    def main() -> None:
        import uvicorn

        uvicorn.run("hr_agent.api.server:app", host=settings.agent_host, port=settings.agent_port, reload=False)

except ImportError:
    app = None

    def main() -> None:
        from hr_agent.api.stdlib_server import main as stdlib_main

        stdlib_main()


if __name__ == "__main__":
    main()
