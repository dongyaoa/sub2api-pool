import http from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { loginWithBrowser } from './browser.mjs';
import { WorkerError, safeError } from './errors.mjs';
import { validateLogin } from './validation.mjs';

function authorized(header, token) {
  const supplied = Buffer.from(typeof header === 'string' ? header : '');
  const expected = Buffer.from(`Bearer ${token}`);
  return supplied.length === expected.length && timingSafeEqual(supplied, expected);
}

function reply(response, status, body) {
  if (response.destroyed || response.writableEnded) return;
  response.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store', 'x-content-type-options': 'nosniff' });
  response.end(JSON.stringify(body));
}

async function readJSON(request) {
  if (!/^application\/json(?:\s*;|$)/i.test(request.headers['content-type'] || '')) throw new WorkerError('invalid_request', 400);
  if (request.headers['content-encoding']) throw new WorkerError('invalid_request', 400);
  let length = 0;
  const chunks = [];
  for await (const chunk of request) {
    length += chunk.length;
    if (length > 32_768) throw new WorkerError('payload_too_large', 413);
    chunks.push(chunk);
  }
  try { return JSON.parse(Buffer.concat(chunks).toString('utf8')); }
  catch { throw new WorkerError('invalid_request', 400); }
  finally { for (const chunk of chunks) chunk.fill(0); }
}

export function createWorkerServer({ token, maxConcurrency = 1, login = loginWithBrowser, timeoutMs = 180_000 } = {}) {
  if (typeof token !== 'string' || token.length < 32 || /\s/.test(token)) throw new WorkerError('invalid_worker_token');
  if (![1, 2].includes(maxConcurrency)) throw new WorkerError('invalid_concurrency');
  let active = 0;
  const controllers = new Set();
  const server = http.createServer(async (request, response) => {
    if (request.method === 'GET' && request.url === '/health') return reply(response, 200, { status: 'ok' });
    if (request.method !== 'POST' || request.url !== '/login') return reply(response, 404, { error_code: 'not_found' });
    if (!authorized(request.headers.authorization, token)) return reply(response, 401, { error_code: 'unauthorized' });
    if (active >= maxConcurrency) return reply(response, 429, { error_code: 'busy' });
    active++;
    const controller = new AbortController();
    controllers.add(controller);
    const abort = () => controller.abort();
    request.on('aborted', abort);
    response.on('close', abort);
    let input;
    try {
      input = validateLogin(await readJSON(request));
      const result = await login(input, { signal: controller.signal, timeoutMs });
      reply(response, 200, result);
    } catch (error) {
      const safe = safeError(error);
      reply(response, safe.status, { error_code: safe.code });
    } finally {
      active--;
      controllers.delete(controller);
      request.off('aborted', abort);
      response.off('close', abort);
      // Do not retain credentials between requests. JS strings cannot be wiped;
      // no browser profile, cookie jar, screenshots, traces, or request logs persist.
      if (input) for (const key of Object.keys(input)) input[key] = undefined;
    }
  });
  server.headersTimeout = 10_000;
  server.requestTimeout = timeoutMs + 15_000;
  server.setTimeout(timeoutMs + 15_000, socket => socket.destroy());
  server.on('clientError', (_error, socket) => socket.destroy());
  server.cancelJobs = () => { for (const controller of controllers) controller.abort(); };
  return server;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const server = createWorkerServer({ token: process.env.OPENAI_REAUTH_WORKER_TOKEN,
      maxConcurrency: Number(process.env.OPENAI_REAUTH_WORKER_CONCURRENCY || 1) });
    const host = process.env.HOST || '127.0.0.1';
    const port = Number(process.env.PORT || 8091);
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new WorkerError('invalid_port');
    server.listen(port, host);
    server.on('error', () => { process.stderr.write('worker_start_failed\n'); process.exitCode = 1; });
    for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => {
      server.cancelJobs();
      server.close();
      server.closeIdleConnections();
    });
  } catch (error) {
    process.stderr.write(`${safeError(error).code}\n`);
    process.exitCode = 1;
  }
}
