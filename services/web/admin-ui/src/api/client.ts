import { AppError, ApiError, toAppError } from "@/errors";

const TOKEN_KEY = "hr_api_token";

export function getApiToken(): string {
  return sessionStorage.getItem(TOKEN_KEY) || "";
}

export function setApiToken(token: string) {
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
}

export { AppError, ApiError };

type ApiOptions = {
  method?: string;
  json?: unknown;
  body?: BodyInit;
  /** HTTP statuses treated as success (e.g. 202 Accepted) */
  acceptStatuses?: number[];
};

function extractErrorPayload(data: unknown, statusText: string): { message: string; code?: string } {
  if (data == null || data === "") {
    return { message: statusText || "请求失败" };
  }
  if (typeof data === "string") {
    return { message: data.trim() || statusText || "请求失败" };
  }
  if (typeof data === "object") {
    const o = data as Record<string, unknown>;
    const msg =
      (typeof o.error === "string" && o.error) ||
      (typeof o.message === "string" && o.message) ||
      (typeof o.msg === "string" && o.msg) ||
      "";
    const code =
      (typeof o.code === "string" && o.code) ||
      (typeof o.human_reason_code === "string" && o.human_reason_code) ||
      (typeof o.code === "number" ? String(o.code) : undefined);
    return { message: msg || statusText || "请求失败", code };
  }
  return { message: statusText || "请求失败" };
}

/** Central fetch interceptor: always throws AppError on failure. */
export async function api<T = unknown>(path: string, opts: ApiOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getApiToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  if (opts.json !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(path, {
      method: opts.method || "GET",
      headers,
      body: opts.json !== undefined ? JSON.stringify(opts.json) : opts.body,
      credentials: "include",
    });
  } catch (e) {
    throw toAppError(e, { fallback: "网络异常，请确认服务已启动", status: 0, code: "network_error" });
  }

  const rawText = await res.text();
  let data: unknown = {};
  if (rawText) {
    try {
      data = JSON.parse(rawText);
    } catch {
      data = rawText;
    }
  }

  const accepted = new Set([200, 201, ...(opts.acceptStatuses || [])]);
  if (!res.ok && !accepted.has(res.status)) {
    const { message, code } = extractErrorPayload(data, res.statusText);
    throw toAppError(message || `HTTP ${res.status}`, {
      status: res.status,
      code,
      technical: typeof data === "string" ? data : rawText,
      fallback: `请求失败（HTTP ${res.status}）`,
    });
  }
  return data as T;
}

export type AuthMe = {
  auth: string;
  is_admin?: boolean;
  can_manage_question_bank?: boolean;
  user?: {
    name?: string;
    is_admin?: boolean;
    can_manage_question_bank?: boolean;
  };
};

export type AdminStats = {
  total?: number;
  created_last_7d?: number;
  awaiting_reply?: number;
  confirmed?: number;
  declined?: number;
  needs_human?: number;
  rejected?: number;
  by_status?: Record<string, number>;
};
