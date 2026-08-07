from __future__ import annotations

from typing import Annotated, TypedDict

from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from langchain_core.messages import AIMessage, BaseMessage, HumanMessage, SystemMessage, ToolMessage
from langchain_core.tools import tool

from hr_agent.agents.parse_heuristics import heuristic_profile, validate_profile
from hr_agent.config.settings import settings
from hr_agent.state.models import CandidateProfile
from hr_agent.tools import llm
from hr_agent.tools.parse_docs import extract_text, ocr_stub
from hr_agent.tools.pii import redact


@tool
def re_extract_text(path: str) -> str:
    """Re-run deterministic text extraction from resume file path."""
    return extract_text(path)


@tool
def run_ocr(path: str) -> str:
    """OCR fallback for scanned or image-like resumes."""
    return ocr_stub(path)


TOOLS = [re_extract_text, run_ocr]
TOOL_MAP = {t.name: t for t in TOOLS}


class ReactState(TypedDict):
    messages: Annotated[list[BaseMessage], add_messages]
    resume_path: str
    raw_text: str
    profile: dict
    steps: int


def agent_node(state: ReactState) -> dict:
    if not llm.has_llm():
        profile = heuristic_profile(state.get("raw_text", ""))
        return {
            "profile": profile.model_dump(),
            "messages": [AIMessage(content="offline heuristic parse")],
            "steps": state.get("steps", 0) + 1,
        }

    ep = settings.parse
    lc = llm._chat_model(ep.model, ep.api_key, ep.api_base)
    if lc is None:
        profile = heuristic_profile(state.get("raw_text", ""))
        return {
            "profile": profile.model_dump(),
            "messages": [AIMessage(content="LLM client unavailable")],
            "steps": state.get("steps", 0) + 1,
        }
    model = lc.bind_tools(TOOLS)
    sys = SystemMessage(
        content=(
            "你是简历解析纠错 Agent。根据 raw 文本抽取结构化候选人信息。"
            "若文本缺失/乱码/年限矛盾，可调用 re_extract_text 或 run_ocr。"
            f"简历路径: {state.get('resume_path', '')}"
        )
    )
    msgs = [sys] + state["messages"]
    resp = model.invoke(msgs)
    return {"messages": [resp], "steps": state.get("steps", 0) + 1}


def tool_node(state: ReactState) -> dict:
    last = state["messages"][-1]
    outs: list[BaseMessage] = []
    raw = state.get("raw_text", "")
    for call in getattr(last, "tool_calls", []) or []:
        name = call["name"]
        args = call.get("args", {})
        tool = TOOL_MAP[name]
        result = tool.invoke(args)
        if name == "re_extract_text" and isinstance(result, str) and result.strip():
            raw = result
        outs.append(ToolMessage(content=str(result)[:4000], tool_call_id=call["id"]))
    return {"messages": outs, "raw_text": raw}


def finalize_node(state: ReactState) -> dict:
    raw = state.get("raw_text", "")
    seed = heuristic_profile(raw)
    if llm.has_llm():
        try:
            schema_hint = (
                "严格输出 CandidateProfile JSON，字段固定为："
                "name,email,phone,education[{school,degree,major,end_year}],"
                "experiences[{company,title,years,highlights}],skills,papers,"
                "total_years,raw_text_excerpt,parse_confidence,issues。"
                "不要用「未知/N/A」占位；识别不到就用空字符串或空数组，并把原因写入 issues。"
                "education/experiences 必须尽量从文本抽取真实学校/公司/职位。"
            )
            profile = llm.structured_invoke(
                settings.parse_model,
                schema_hint,
                redact(raw[:12000]),
                CandidateProfile,
            )
            # Prefer heuristic education/experience when model returns empty placeholders.
            if not profile.education and seed.education:
                profile.education = seed.education
            if (not profile.experiences or not any(e.company or e.title for e in profile.experiences)) and seed.experiences:
                profile.experiences = seed.experiences
            if not profile.skills and seed.skills:
                profile.skills = seed.skills
            if not profile.name and seed.name:
                profile.name = seed.name
        except Exception:
            profile = seed
    else:
        profile = seed
    profile.issues = validate_profile(profile)
    return {"profile": profile.model_dump()}


def should_continue(state: ReactState) -> str:
    if state.get("steps", 0) >= settings.react_max_steps:
        return "finalize"
    last = state["messages"][-1] if state.get("messages") else None
    if isinstance(last, AIMessage) and getattr(last, "tool_calls", None):
        return "tools"
    return "finalize"


def build_parse_react_graph():
    g = StateGraph(ReactState)
    g.add_node("agent", agent_node)
    g.add_node("tools", tool_node)
    g.add_node("finalize", finalize_node)
    g.add_edge(START, "agent")
    g.add_conditional_edges("agent", should_continue, {"tools": "tools", "finalize": "finalize"})
    g.add_edge("tools", "agent")
    g.add_edge("finalize", END)
    return g.compile()


parse_react_graph = build_parse_react_graph()


def run_parse_correct(resume_path: str, resume_text: str = "") -> tuple[str, CandidateProfile]:
    raw = extract_text(resume_path, resume_text)
    init: ReactState = {
        "messages": [HumanMessage(content=f"请解析并校验以下简历文本：\n{redact(raw[:8000])}")],
        "resume_path": resume_path,
        "raw_text": raw,
        "profile": {},
        "steps": 0,
    }
    out = parse_react_graph.invoke(init)
    profile = CandidateProfile.model_validate(out.get("profile") or heuristic_profile(raw).model_dump())
    return out.get("raw_text", raw), profile
