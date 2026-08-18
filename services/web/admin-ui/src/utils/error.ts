/**
 * @deprecated Prefer `@/errors` + `useNotify().from(err)`.
 * Kept so existing imports keep working.
 */
export { formatError, toAppError, AppError, ApiError } from "@/errors";

import { toAppError } from "@/errors";

type MessageErrorApi = {
  error: (content: string, options?: { duration?: number; keepAliveOnHover?: boolean }) => unknown;
};

/** @deprecated use `useNotify().from(err)` */
export function toastError(message: MessageErrorApi, err: unknown, fallback?: string) {
  const appErr = toAppError(err, fallback);
  const duration = Math.min(16000, Math.max(5000, 3000 + appErr.userMessage.length * 40));
  message.error(appErr.userMessage, { duration, keepAliveOnHover: true });
  if (import.meta.env.DEV) {
    console.error("[AppError]", appErr.code, appErr.status, appErr.technical, err);
  }
}
