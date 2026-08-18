import { api, getApiToken } from "@/api/client";
import { toAppError } from "@/errors";

function throwHttpError(res: Response, raw: string, data: Record<string, unknown>) {
  throw toAppError(String(data.error || raw || res.statusText || "请求失败"), {
    status: res.status,
    technical: raw,
  });
}

export type ApplicationSummary = {
  id: string;
  candidate_name?: string;
  candidate_email?: string;
  status: string;
  jd_id?: string;
  updated_at?: string;
  error_kind?: string;
  system_error_code?: string;
  human_reason_code?: string;
};

export type InterviewRoundReq = {
  id?: string;
  role_kind?: string;
  headcount?: number;
  match_jd_department?: boolean;
  specialties?: string[];
  fixed_open_ids?: string[];
  pool_id?: string;
};

export type RoundFeedback = {
  rating?: number;
  summary?: string;
  recommend?: string;
  by?: string;
  at?: string;
  note?: string;
};

export type InterviewRound = {
  id?: string;
  round_index?: number;
  sort_order?: number;
  round_key?: string;
  name?: string;
  theme?: string;
  duration_minutes?: number;
  advance?: string;
  status?: string;
  assigned_open_ids?: string[];
  provider_event_id?: string;
  outcome?: string;
  feedback_json?: RoundFeedback;
  requirements?: InterviewRoundReq[];
};

export type OfferStatus = "none" | "pending" | "sent" | "accepted" | "declined" | "hired";

export type ApplicationDetail = ApplicationSummary & {
  error_message?: string;
  screen_tier?: string;
  contact_email_source?: string;
  contact_email_confidence?: number;
  has_resume?: boolean;
  resume_name?: string;
  current_round_index?: number;
  interview_rounds?: InterviewRound[];
  offer_status?: OfferStatus | string;
  offer_note?: string;
  offer_updated_at?: string;
  hired_at?: string;
  profile?: Record<string, unknown>;
  screen?: Record<string, unknown>;
  questions?: { category?: string; question?: string }[];
  slots?: { status?: string; starts_at?: string; ends_at?: string; location?: string }[];
  audit?: {
    action: string;
    actor?: string;
    after_status?: string;
    created_at?: string;
    detail?: { reason?: string; error?: string };
  }[];
  jd?: { title?: string; department?: string };
};

export type JDItem = {
  id: string;
  title?: string;
  department?: string;
  salary?: string;
  location?: string;
  description?: string;
  created_at?: string;
  rounds?: InterviewRound[];
  round_count?: number;
};

export type QuestionBankItem = {
  id: string;
  title?: string;
  category?: string;
  difficulty?: string;
  enabled?: boolean;
  tags?: string[];
  content?: string;
};

export type StaffMember = {
  open_id: string;
  name?: string;
  email?: string;
  is_hr?: boolean;
  is_interviewer?: boolean;
  is_admin?: boolean;
  can_manage_question_bank?: boolean;
  enabled?: boolean;
};

export async function listApplications(status?: string) {
  const q = status ? `?status=${encodeURIComponent(status)}` : "";
  return api<{ items: ApplicationSummary[] }>(`/v1/admin/applications${q}`);
}

export async function listApplicationsByErrorKind(errorKind: string) {
  return api<{ items: ApplicationSummary[] }>(
    `/v1/admin/applications?error_kind=${encodeURIComponent(errorKind)}`,
  );
}

export async function getApplication(id: string) {
  return api<ApplicationDetail>(`/v1/admin/applications/${id}`);
}

export async function retryParse(id: string, resumeText?: string) {
  return api(`/v1/admin/applications/${id}/retry-parse`, {
    method: "POST",
    json: resumeText ? { resume_text: resumeText } : {},
  });
}

export async function humanApprove(id: string) {
  return api(`/v1/admin/applications/${id}/human/approve`, { method: "POST", json: {} });
}

export async function createApplicationJson(body: Record<string, unknown>) {
  return api<{ application_id?: string; application?: ApplicationDetail }>(
    "/v1/admin/applications",
    { method: "POST", json: body },
  );
}

export async function createApplicationMultipart(fd: FormData) {
  const headers: Record<string, string> = {};
  const token = getApiToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch("/v1/admin/applications", {
    method: "POST",
    headers,
    body: fd,
    credentials: "include",
  });
  const raw = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = raw ? (JSON.parse(raw) as Record<string, unknown>) : {};
  } catch {
    if (!res.ok && res.status !== 202) throwHttpError(res, raw, {});
  }
  if (!res.ok && res.status !== 202) throwHttpError(res, raw, data);
  return data as { application_id?: string; application?: ApplicationDetail };
}

export async function createImport(fd: FormData) {
  const headers: Record<string, string> = {};
  const token = getApiToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch("/v1/admin/imports", { method: "POST", headers, body: fd, credentials: "include" });
  const raw = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = raw ? (JSON.parse(raw) as Record<string, unknown>) : {};
  } catch {
    if (!res.ok && res.status !== 202) throwHttpError(res, raw, {});
  }
  if (!res.ok && res.status !== 202) throwHttpError(res, raw, data);
  return data as { job_id: string; total?: number };
}

export async function listImportItems(jobId: string, status?: string) {
  const q = status ? `?status=${encodeURIComponent(status)}` : "";
  return api<{ items: ImportItemRow[] }>(`/v1/admin/imports/${jobId}/items${q}`);
}

export async function retryImportItem(jobId: string, itemId: string) {
  return api(`/v1/admin/imports/${jobId}/items/${itemId}/retry`, { method: "POST", json: {} });
}

export type ImportItemRow = {
  id: string;
  status: string;
  candidate_name?: string;
  candidate_email?: string;
  email_source?: string;
  external_id?: string;
  application_id?: string;
  error_message?: string;
};

export async function getImportJob(id: string) {
  return api<{ total?: number; succeeded?: number; failed?: number; status?: string }>(
    `/v1/admin/imports/${id}`,
  );
}

export async function listJds() {
  return api<{ items: JDItem[] }>("/v1/admin/jds");
}

export async function getJd(id: string) {
  return api<JDItem>(`/v1/admin/jds/${id}`);
}

export async function upsertJd(body: Record<string, unknown>, id?: string) {
  if (id) {
    return api(`/v1/admin/jds/${id}`, { method: "PUT", json: body });
  }
  return api<{ id: string }>("/v1/admin/jds", { method: "POST", json: body });
}

export async function deleteJd(id: string) {
  return api(`/v1/admin/jds/${id}`, { method: "DELETE" });
}

export async function putInterviewPlan(jdId: string, rounds: InterviewRound[]) {
  return api<{ ok: boolean; rounds?: InterviewRound[] }>(`/v1/admin/jds/${jdId}/interview-plan`, {
    method: "PUT",
    json: { rounds },
  });
}

export async function advanceRound(
  appId: string,
  index: number,
  outcome: "pass" | "fail" | "hold",
  note?: string,
  feedback?: { rating?: number; summary?: string; recommend?: string },
) {
  return api<{ ok: boolean; application?: ApplicationDetail }>(
    `/v1/admin/applications/${appId}/rounds/${index}/advance`,
    {
      method: "POST",
      json: {
        outcome,
        note: note || "",
        ...(feedback ? { feedback } : {}),
      },
    },
  );
}

export async function updateOffer(
  appId: string,
  status: OfferStatus | string,
  note?: string,
) {
  return api<{ ok: boolean }>(`/v1/admin/applications/${appId}/offer`, {
    method: "POST",
    json: { status, note: note || "" },
  });
}

export type InterviewerProfile = {
  open_id: string;
  name?: string;
  email?: string;
  department?: string;
  role_kinds?: string[];
  specialties?: string[];
  enabled?: boolean;
  notes?: string;
  updated_at?: string;
};

export type InterviewerPool = {
  id: string;
  name?: string;
  default_role_kind?: string;
  department?: string;
  enabled?: boolean;
  notes?: string;
  member_open_ids?: string[];
  member_count?: number;
  updated_at?: string;
};

export type FeishuUserHit = {
  open_id: string;
  name?: string;
  email?: string;
  department?: string;
};

export async function searchFeishuUsers(q: string, limit = 20) {
  const qs = new URLSearchParams({ q, limit: String(limit) });
  return api<{ items: FeishuUserHit[] }>(`/v1/admin/feishu/users?${qs}`);
}

export async function listInterviewers(params?: { role_kind?: string; department?: string }) {
  const q = new URLSearchParams();
  if (params?.role_kind) q.set("role_kind", params.role_kind);
  if (params?.department) q.set("department", params.department);
  const qs = q.toString();
  return api<{ items: InterviewerProfile[]; role_kinds?: string[] }>(
    `/v1/admin/interviewers${qs ? `?${qs}` : ""}`,
  );
}

export async function saveInterviewer(body: {
  open_id: string;
  name?: string;
  email?: string;
  department?: string;
  role_kinds?: string[];
  specialties?: string[];
  enabled?: boolean;
  notes?: string;
}) {
  return api<InterviewerProfile>(`/v1/admin/interviewers/${encodeURIComponent(body.open_id)}`, {
    method: "PUT",
    json: body,
  });
}

export async function setInterviewerEnabled(openId: string, enabled: boolean) {
  const path = enabled ? "enable" : "disable";
  return api(`/v1/admin/interviewers/${encodeURIComponent(openId)}/${path}`, {
    method: "POST",
    json: {},
  });
}

export async function listInterviewerPools() {
  return api<{ items: InterviewerPool[] }>("/v1/admin/interviewer-pools");
}

export async function saveInterviewerPool(body: {
  id?: string;
  name: string;
  default_role_kind?: string;
  department?: string;
  enabled?: boolean;
  notes?: string;
  member_open_ids?: string[];
}) {
  return api<InterviewerPool>("/v1/admin/interviewer-pools", { method: "PUT", json: body });
}

export async function deleteInterviewerPool(id: string) {
  return api(`/v1/admin/interviewer-pools/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function manualSchedule(
  appId: string,
  body: {
    assigned_open_ids?: string[];
    slots?: { starts_at: string; ends_at: string; location?: string }[];
    resend_invite?: boolean;
  },
) {
  return api<{ ok: boolean; application?: ApplicationDetail }>(
    `/v1/admin/applications/${appId}/manual-schedule`,
    { method: "POST", json: body },
  );
}

export async function listQuestionBank() {
  return api<{ items: QuestionBankItem[] }>("/v1/admin/question-bank");
}

export async function getQuestionBankItem(id: string) {
  return api<QuestionBankItem>(`/v1/admin/question-bank/${id}`);
}

export async function saveQuestionBank(body: Record<string, unknown>, id?: string) {
  if (id) return api(`/v1/admin/question-bank/${id}`, { method: "PUT", json: body });
  return api("/v1/admin/question-bank", { method: "POST", json: body });
}

export async function deleteQuestionBank(id: string) {
  return api(`/v1/admin/question-bank/${id}`, { method: "DELETE" });
}

export async function reindexQuestionBank() {
  return api<{ ok?: boolean; synced_items?: number; upserted?: number; error?: string }>(
    "/v1/admin/question-bank/reindex",
    { method: "POST", json: {} },
  );
}

export async function batchQuestionBankJson(items: Record<string, unknown>[]) {
  return api<{ succeeded?: number; failed?: number; total?: number; errors?: string[] }>(
    "/v1/admin/question-bank/batch",
    { method: "POST", json: { items } },
  );
}

export async function batchQuestionBankCsv(file: File, reindexAfter = false) {
  const fd = new FormData();
  fd.append("csv", file);
  const headers: Record<string, string> = {};
  const token = getApiToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch("/v1/admin/question-bank/batch", {
    method: "POST",
    headers,
    body: fd,
    credentials: "include",
  });
  const raw = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = raw ? (JSON.parse(raw) as Record<string, unknown>) : {};
  } catch {
    if (!res.ok) throwHttpError(res, raw, {});
  }
  if (!res.ok) throwHttpError(res, raw, data);
  if (reindexAfter) {
    await reindexQuestionBank();
  }
  return data as { succeeded?: number; failed?: number; total?: number; errors?: string[] };
}

export async function listStaff() {
  return api<{ items: StaffMember[] }>("/v1/admin/staff");
}

export async function saveStaff(body: Record<string, unknown>, openId?: string) {
  if (openId) {
    return api(`/v1/admin/staff/${encodeURIComponent(openId)}`, { method: "PUT", json: body });
  }
  return api("/v1/admin/staff", { method: "POST", json: body });
}

export async function setStaffEnabled(openId: string, enabled: boolean) {
  const path = enabled ? "enable" : "disable";
  return api(`/v1/admin/staff/${encodeURIComponent(openId)}/${path}`, { method: "POST", json: {} });
}

export async function listJoinRequests() {
  return api<{ items: { id: string; name?: string; email?: string; open_id?: string; created_at?: string }[]; pending_count?: number }>(
    "/v1/admin/staff/join-requests?status=pending",
  ).catch(() => ({ items: [], pending_count: 0 }));
}

export async function approveJoin(id: string, body: Record<string, unknown>) {
  return api(`/v1/admin/staff/join-requests/${encodeURIComponent(id)}/approve`, {
    method: "POST",
    json: body,
  });
}

export async function rejectJoin(id: string) {
  return api(`/v1/admin/staff/join-requests/${encodeURIComponent(id)}/reject`, {
    method: "POST",
    json: { note: "rejected" },
  });
}

export async function staffAudit() {
  return api<{ items: { action: string; actor?: string; created_at?: string; detail?: Record<string, unknown> }[] }>(
    "/v1/admin/staff/audit",
  ).catch(() => ({ items: [] }));
}

export async function updateApplicationContact(id: string, email: string, name: string) {
  return api(`/v1/admin/applications/${id}/contact`, {
    method: "PUT",
    json: { email, name },
  });
}

export function resumeDownloadUrl(id: string) {
  return `/v1/admin/applications/${encodeURIComponent(id)}/resume`;
}

export const CONTACT_SOURCE_LABEL: Record<string, string> = {
  hr_form: "表单填写",
  import_csv: "CSV 映射",
  import_placeholder: "批量占位",
  pre_extract: "预抽取",
  parse_profile: "解析回填",
  human_override: "人工修改",
  candidate_self: "候选人自填",
};
