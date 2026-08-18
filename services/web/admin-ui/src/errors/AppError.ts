/** Normalized application error thrown by API layer and shown via useNotify. */
export class AppError extends Error {
  /** HTTP status; 0 = network / client */
  status: number;
  /** Stable business / HTTP code when known */
  code?: string;
  /** Short Chinese text for HR UI */
  userMessage: string;
  /** Raw upstream text for console / dev only */
  technical?: string;

  constructor(opts: {
    userMessage: string;
    status?: number;
    code?: string;
    technical?: string;
    cause?: unknown;
  }) {
    super(opts.userMessage);
    this.name = "AppError";
    this.status = opts.status ?? 0;
    this.code = opts.code;
    this.userMessage = opts.userMessage;
    this.technical = opts.technical;
    if (opts.cause !== undefined) {
      (this as Error & { cause?: unknown }).cause = opts.cause;
    }
  }
}

/** @deprecated use AppError — kept for gradual migration */
export class ApiError extends AppError {
  constructor(message: string, status: number, code?: string) {
    super({
      userMessage: message,
      status,
      code,
      technical: message,
    });
    this.name = "ApiError";
  }
}
