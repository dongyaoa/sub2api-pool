export class WorkerError extends Error {
  constructor(code, status = 422) {
    super(code);
    this.code = code;
    this.status = status;
  }
}

export function safeError(error) {
  return error instanceof WorkerError ? error : new WorkerError('login_failed', 502);
}
