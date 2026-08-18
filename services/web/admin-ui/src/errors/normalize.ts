import { AppError } from "@/errors/AppError";
import { catalogLookup, ERROR_CATALOG, HTTP_STATUS_CATALOG } from "@/errors/catalog";

function compactTail(s: string, max = 160): string {
  const one = s.replace(/\s+/g, " ").trim();
  if (one.length <= max) return one;
  return one.slice(0, max) + "…";
}

function extractInvalidOpenIds(raw: string): string[] {
  const ids = new Set<string>();
  const bracket = raw.match(/Invalid ids:\s*\[([^\]]+)\]/i);
  if (bracket) {
    for (const part of bracket[1].split(",")) {
      const id = part.replace(/["'\s]/g, "");
      if (id.startsWith("ou_")) ids.add(id);
    }
  }
  for (const m of raw.matchAll(/"value"\s*:\s*"(ou_[^"]+)"/g)) ids.add(m[1]);
  for (const m of raw.matchAll(/\b(ou_smoke_[a-zA-Z0-9_]+)\b/g)) ids.add(m[1]);
  return [...ids];
}

function humanizeFeishu(raw: string): { code: string; message: string } | null {
  const s = raw.trim();
  if (!s) return null;
  const lower = s.toLowerCase();
  const invalidIds = extractInvalidOpenIds(s);
  const hasInvalidOpenId =
    /99992351/.test(s) ||
    /not a valid\s*\{?open_id\}?/i.test(s) ||
    /id not exist/i.test(s) ||
    /invalid ids/i.test(s) ||
    invalidIds.length > 0;

  if (hasInvalidOpenId) {
    const smoke = invalidIds.filter((id) => id.includes("smoke"));
    const show = (smoke.length ? smoke : invalidIds).slice(0, 3).join("、");
    if (smoke.length || /ou_smoke_/i.test(s)) {
      const base = ERROR_CATALOG.feishu_smoke_open_id;
      return {
        code: "feishu_smoke_open_id",
        message: show ? `当前面试官 open_id（${show}）是测试占位符，飞书不认。请到「面试官档案」用通讯录搜索换成真实员工，再重新排期` : base,
      };
    }
    const base = ERROR_CATALOG.feishu_invalid_open_id;
    return {
      code: "feishu_invalid_open_id",
      message: show ? `飞书不认识这些面试官 ID：${show}。请到「面试官档案」用通讯录重新选人并保存，再排期` : base,
    };
  }

  if (/99991663|invalid access token/i.test(s)) {
    return { code: "feishu_auth", message: ERROR_CATALOG.feishu_auth };
  }
  if (/99991672|no permission|scope|无权限|contact:user/i.test(s) && /feishu|飞书|contact/i.test(s)) {
    return { code: "feishu_permission", message: ERROR_CATALOG.feishu_permission };
  }
  if (/feishu\s*http\s*\d+/i.test(s) || lower.includes("open.feishu.cn") || /"code"\s*:\s*9999/.test(s)) {
    try {
      const m = s.match(/\{[\s\S]*\}/);
      if (m) {
        const j = JSON.parse(m[0]) as { code?: number; msg?: string };
        if (j.code === 99992351) {
          return { code: "feishu_invalid_open_id", message: ERROR_CATALOG.feishu_invalid_open_id };
        }
      }
    } catch {
      /* ignore */
    }
    return { code: "feishu_generic", message: ERROR_CATALOG.feishu_generic };
  }
  return null;
}

function userMessageFromRaw(raw: string, fallback: string): { code?: string; userMessage: string } {
  const feishu = humanizeFeishu(raw);
  if (feishu) return { code: feishu.code, userMessage: feishu.message };

  const byCode = catalogLookup(raw);
  if (byCode) return { userMessage: byCode };

  const parts = raw.split("；");
  if (parts.length > 1 && /[\u4e00-\u9fff]/.test(parts[parts.length - 1])) {
    const tip = parts[parts.length - 1].trim();
    const headMapped = catalogLookup(parts[0].trim()) || humanizeFeishu(parts[0])?.message;
    if (headMapped) return { userMessage: headMapped };
    if (/[\u4e00-\u9fff]/.test(tip)) return { userMessage: tip };
  }

  if (raw.includes('{"code"') || raw.includes('"msg"') || raw.includes("field_violations")) {
    return { userMessage: "服务返回了技术错误，请检查面试官 open_id / 飞书权限后重试" };
  }
  if (raw.length > 120 && !/[\u4e00-\u9fff]/.test(raw)) {
    return { userMessage: "操作失败（上游返回了英文技术错误）。请检查配置或查看运行记录" };
  }
  if (/[\u4e00-\u9fff]/.test(raw)) return { userMessage: compactTail(raw, 160) };
  return { userMessage: fallback };
}

export type NormalizeOptions = {
  fallback?: string;
  status?: number;
  code?: string;
  technical?: string;
};

/** Normalize any thrown value into AppError (userMessage never dumps raw Feishu JSON). */
export function toAppError(err: unknown, opts: NormalizeOptions | string = {}): AppError {
  const options: NormalizeOptions = typeof opts === "string" ? { fallback: opts } : opts;
  const fallback = options.fallback || "操作失败，请稍后重试";

  if (err instanceof AppError) {
    if (options.fallback && err.userMessage === err.message && !err.technical) {
      return err;
    }
    return err;
  }

  if (typeof err === "string") {
    const { code, userMessage } = userMessageFromRaw(err, fallback);
    return new AppError({
      userMessage: userMessage || fallback,
      code: options.code || code,
      status: options.status ?? 0,
      technical: options.technical || err,
      cause: err,
    });
  }

  if (err instanceof TypeError && /fetch|network|Failed to fetch/i.test(err.message)) {
    return new AppError({
      userMessage: ERROR_CATALOG.network_error,
      code: "network_error",
      status: 0,
      technical: err.message,
      cause: err,
    });
  }

  if (err instanceof Error) {
    const raw = (err.message || "").trim();
    if (/Failed to fetch|NetworkError|Load failed/i.test(raw)) {
      return new AppError({
        userMessage: ERROR_CATALOG.network_error,
        code: "network_error",
        status: 0,
        technical: raw,
        cause: err,
      });
    }
    const errRec = err as unknown as { status?: number; code?: string };
    const status = typeof errRec.status === "number" ? errRec.status : options.status ?? 0;
    const code = options.code || (typeof errRec.code === "string" ? errRec.code : undefined);
    const fromCode = catalogLookup(code);
    if (fromCode) {
      return new AppError({
        userMessage: fromCode,
        code,
        status,
        technical: raw || options.technical,
        cause: err,
      });
    }
    const { code: inferred, userMessage } = userMessageFromRaw(raw || "", fallback);
    const byStatus = status ? HTTP_STATUS_CATALOG[status] : undefined;
    return new AppError({
      userMessage: userMessage || byStatus || fallback,
      code: code || inferred,
      status,
      technical: raw || options.technical,
      cause: err,
    });
  }

  if (options.status && HTTP_STATUS_CATALOG[options.status]) {
    return new AppError({
      userMessage: HTTP_STATUS_CATALOG[options.status],
      status: options.status,
      code: options.code,
      technical: options.technical,
      cause: err,
    });
  }

  return new AppError({
    userMessage: fallback,
    status: options.status ?? 0,
    code: options.code,
    technical: options.technical || String(err),
    cause: err,
  });
}

/** User-facing string only (for alerts / captions). */
export function formatError(err: unknown, fallback = "操作失败，请稍后重试"): string {
  if (err == null || err === "") return fallback;
  return toAppError(err, fallback).userMessage || fallback;
}
