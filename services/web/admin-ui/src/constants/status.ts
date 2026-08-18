import { formatError } from "@/errors";

export const STATUS_LABEL: Record<string, string> = {
  awaiting_reply: "待回复",
  confirmed: "已确认",
  declined: "已拒绝",
  needs_human: "需人工",
  rejected: "未通过",
  screened: "已筛选",
  parsing: "解析中",
  screening: "筛选中",
  failed: "失败",
  uploaded: "已上传",
  questions_ready: "题目就绪",
};

export const ACTION_LABEL: Record<string, string> = {
  application_created: "投递入库",
  parse_started: "简历解析 Agent 开始",
  screen_started: "岗位筛选 Agent 开始",
  screened: "筛选 Agent 通过",
  rejected_by_screen: "筛选 Agent 未通过",
  needs_human_after_parse: "解析不足，转人工接管",
  invite_preparing: "约面 Agent：准备邀约",
  invite_enqueued: "约面 Agent：邀约已入队",
  retry_parse: "重试解析 Agent",
  pipeline_failed: "流水线失败",
  human_approved: "人工通过并约面",
  interview_confirmed: "候选人已确认面试",
  interview_declined: "候选人已拒绝",
  questions_generated: "出题 Agent 完成",
  questions_failed: "出题 Agent 失败",
  needs_human: "转人工接管",
  round_pass: "本轮通过",
  round_fail: "本轮淘汰",
  round_hold: "本轮暂缓",
  all_rounds_passed: "全部轮次通过",
  offer_pending: "进入待发 Offer",
  offer_sent: "已发 Offer",
  offer_accepted: "Offer 已接受",
  offer_declined: "Offer 已拒绝",
  offer_hired: "标记入职",
  email_reply_needs_human: "邮件回复转人工",
  manual_assignees_updated: "更新面试官指派",
  manual_schedule_saved: "人工排期已保存",
  manual_invite_enqueued: "人工重发邀约",
  needs_human_after_scheduling: "排期指派校验失败",
};

export type AgentStepState = "done" | "current" | "pending" | "failed" | "human";

export type AgentPipelineStep = {
  key: string;
  label: string;
  state: AgentStepState;
};

export type AgentPipeline = {
  steps: AgentPipelineStep[];
  currentIndex: number;
  caption: string;
  shortLabel: string;
  mode: "running" | "wait" | "human" | "fail" | "done";
};

type PipelineInput = {
  status?: string;
  profile?: unknown;
  screen?: unknown;
  questions?: unknown;
  offer_status?: string;
  error_message?: string;
  human_reason_code?: string;
};

const PIPELINE_KEYS = [
  { key: "intake", label: "投递" },
  { key: "parse", label: "解析 Agent" },
  { key: "screen", label: "筛选 Agent" },
  { key: "schedule", label: "约面 Agent" },
  { key: "reply", label: "回复" },
  { key: "questions", label: "出题 Agent" },
  { key: "offer", label: "Offer" },
] as const;

const OFFER_SHORT: Record<string, string> = {
  pending: "待发",
  sent: "已发出",
  accepted: "已接受",
  declined: "已拒绝",
  hired: "已入职",
};

/** Derive one-line agent progress from existing application fields (no streaming API). */
export function agentPipeline(app: PipelineInput | null | undefined): AgentPipeline {
  const st = app?.status || "uploaded";
  const hasProfile = !!app?.profile && typeof app.profile === "object";
  const hasScreen = !!app?.screen && typeof app.screen === "object";
  const qs = app?.questions;
  const hasQuestions = Array.isArray(qs) ? qs.length > 0 : !!qs;
  const offer = String(app?.offer_status || "none");
  const offerActive = offer !== "none" && offer !== "";

  let currentIndex = 0;
  let mode: AgentPipeline["mode"] = "wait";
  let caption = "";

  switch (st) {
    case "uploaded":
      currentIndex = 0;
      mode = "running";
      caption = "当前：投递入库 · 等待解析 Agent";
      break;
    case "parsing":
      currentIndex = 1;
      mode = "running";
      caption = "当前：简历解析 Agent · 运行中";
      break;
    case "screening":
      currentIndex = 2;
      mode = "running";
      caption = "当前：岗位筛选 Agent · 运行中";
      break;
    case "failed":
      currentIndex = 1;
      mode = "fail";
      caption = `当前：解析 Agent · 失败${app?.error_message ? ` · ${app.error_message}` : ""}`;
      break;
    case "needs_human":
      currentIndex = hasScreen ? 3 : hasProfile ? 2 : 1;
      mode = "human";
      {
        const why = formatError(app?.human_reason_code || app?.error_message || "", "");
        caption = why ? `当前：需人工接管 · ${why}` : "当前：需人工接管";
      }
      break;
    case "rejected":
      currentIndex = 2;
      mode = "fail";
      caption = "当前：筛选 Agent · 未通过";
      break;
    case "screened":
      currentIndex = 3;
      mode = "running";
      caption = "当前：约面 Agent · 排期中";
      break;
    case "awaiting_reply":
      currentIndex = 4;
      mode = "wait";
      caption = "当前：约面 Agent · 已发邀约 · 待候选人回复";
      break;
    case "declined":
      currentIndex = 4;
      mode = "fail";
      caption = "当前：候选人已拒绝";
      break;
    case "confirmed":
    case "questions_ready":
      if (offerActive) {
        currentIndex = 6;
        mode = offer === "hired" ? "done" : "wait";
        caption = `当前：Offer · ${OFFER_SHORT[offer] || offer}`;
      } else if (hasQuestions || st === "questions_ready") {
        currentIndex = 5;
        mode = "wait";
        caption = "当前：出题 Agent · 已完成 · 面试进行中";
      } else {
        currentIndex = 5;
        mode = "running";
        caption = "当前：出题 Agent · 运行中 / 待生成";
      }
      break;
    default:
      currentIndex = 0;
      mode = "wait";
      caption = `当前：${statusLabel(st)}`;
  }

  const steps: AgentPipelineStep[] = PIPELINE_KEYS.map((p, i) => {
    let state: AgentStepState = "pending";
    if (i < currentIndex) state = "done";
    else if (i > currentIndex) state = "pending";
    else if (mode === "fail") state = "failed";
    else if (mode === "human") state = "human";
    else state = "current";
    return { key: p.key, label: p.label, state };
  });

  const shortLabel = steps[currentIndex]?.label || statusLabel(st);
  return {
    steps,
    currentIndex,
    caption: `${caption} · 第 ${currentIndex + 1}/${steps.length} 步`,
    shortLabel,
    mode,
  };
}

export const ISSUE_LABEL: Record<string, string> = {
  empty_text: "简历正文为空",
  empty_profile: "画像实质为空",
  low_confidence: "置信度过低",
  education_missing: "教育经历缺失",
  experience_missing: "工作经历缺失",
  no_skills: "无技能信息",
  no_years: "年限不明",
  years_unclear: "年限不明",
  contact_missing: "无有效联系邮箱",
  contact_csv_parse_mismatch: "CSV 邮箱与解析不一致",
};

export function statusLabel(s: string | undefined) {
  return STATUS_LABEL[s || ""] || s || "-";
}

export function isProcessingStatus(st: string) {
  return ["uploaded", "parsing", "screening", "screened"].includes(st);
}

export function isTerminalStatus(st: string) {
  return [
    "awaiting_reply",
    "confirmed",
    "declined",
    "rejected",
    "failed",
    "needs_human",
    "questions_ready",
  ].includes(st);
}

export function canRetryParse(status: string) {
  return ["needs_human", "rejected", "failed", "uploaded", "parsing", "screening"].includes(
    status,
  );
}

export function canHumanApprove(status: string) {
  return ["needs_human", "rejected", "failed", "screened", "questions_ready"].includes(status);
}

export function canHumanApproveApp(app: { status: string; error_kind?: string }) {
  return canHumanApprove(app.status) && app.error_kind !== "system";
}

export function stepperIndex(status: string): { index: number; mode: "current" | "fail" | "warn" } {
  switch (status) {
    case "uploaded":
      return { index: 0, mode: "current" };
    case "parsing":
      return { index: 1, mode: "current" };
    case "screening":
      return { index: 2, mode: "current" };
    case "needs_human":
      return { index: 1, mode: "warn" };
    case "failed":
      return { index: 1, mode: "fail" };
    case "rejected":
      return { index: 2, mode: "fail" };
    case "screened":
    case "questions_ready":
      return { index: 3, mode: "current" };
    case "awaiting_reply":
      return { index: 4, mode: "current" };
    case "confirmed":
      return { index: 5, mode: "current" };
    case "declined":
      return { index: 5, mode: "fail" };
    default:
      return { index: 0, mode: "current" };
  }
}

export const STEPPER_LABELS = ["投递", "解析 Agent", "筛选 Agent", "约面 Agent", "回复", "出题/完成"];
