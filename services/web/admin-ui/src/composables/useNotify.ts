import { useMessage } from "naive-ui";
import { toAppError } from "@/errors";

/**
 * Unified toast / notify API for pages.
 * Prefer `notify.from(err)` in catch — never show raw e.message.
 */
export function useNotify() {
  const message = useMessage();

  function success(content: string) {
    message.success(content, { duration: 2800 });
  }

  function warning(content: string) {
    message.warning(content, { duration: 4500, keepAliveOnHover: true });
  }

  function info(content: string) {
    message.info(content, { duration: 3200 });
  }

  function error(content: string) {
    const duration = Math.min(16000, Math.max(5000, 3000 + content.length * 40));
    message.error(content, { duration, keepAliveOnHover: true });
  }

  /** Map any thrown value → catalogued Chinese toast; log technical detail in DEV. */
  function from(err: unknown, fallback?: string) {
    const appErr = toAppError(err, fallback);
    error(appErr.userMessage);
    if (import.meta.env.DEV) {
      console.error("[AppError]", {
        code: appErr.code,
        status: appErr.status,
        userMessage: appErr.userMessage,
        technical: appErr.technical,
        cause: (appErr as Error & { cause?: unknown }).cause ?? err,
      });
    }
    return appErr;
  }

  return { success, warning, info, error, from, message };
}
