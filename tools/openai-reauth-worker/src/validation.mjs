import { WorkerError } from './errors.mjs';
import { decodeSecret } from './totp.mjs';

export const REDIRECT_URI = 'http://localhost:1455/auth/callback';
const CLIENT_ID = 'app_EMoamEEZ73f0CkXaXp7hrann';
const LOGIN_HOSTS = new Set(['auth.openai.com', 'chatgpt.com']);

function parseURL(value) {
  try { return new URL(value); } catch { throw new WorkerError('invalid_request', 400); }
}

export function isCredentialOrigin(value) {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && LOGIN_HOSTS.has(url.hostname) && !url.port && !url.username && !url.password;
  } catch { return false; }
}

export function isAllowedResource(value) {
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.port || url.username || url.password) return false;
    return LOGIN_HOSTS.has(url.hostname) || url.hostname === 'challenges.cloudflare.com'
      || url.hostname === 'cdn.auth0.com' || url.hostname === 'cdn.oaistatic.com'
      || url.hostname === 'openai.com' || url.hostname.endsWith('.openai.com');
  } catch { return false; }
}

export function parseProxy(value) {
  if (typeof value !== 'string' || !value || value.length > 4096) throw new WorkerError('proxy_required', 400);
  const url = parseURL(value);
  if (!['http:', 'https:', 'socks5:', 'socks5h:'].includes(url.protocol) || !url.hostname || url.search || url.hash
    || (url.pathname && url.pathname !== '/')) throw new WorkerError('unsupported_proxy', 400);
  const socks = url.protocol === 'socks5:' || url.protocol === 'socks5h:';
  if (socks && (url.username || url.password)) throw new WorkerError('unsupported_proxy', 400);
  let username, password;
  try { username = decodeURIComponent(url.username); password = decodeURIComponent(url.password); }
  catch { throw new WorkerError('invalid_request', 400); }
  // No direct fallback and no bypass: even loopback traffic must use the chosen proxy.
  return {
    server: `${socks ? 'socks5:' : url.protocol}//${url.host}`,
    ...(username || password ? { username, password } : {}),
    bypass: '<-loopback>',
  };
}

export function validateLogin(body) {
  if (!body || typeof body !== 'object' || Array.isArray(body)) throw new WorkerError('invalid_request', 400);
  for (const field of ['email', 'password', 'totp_secret', 'auth_url', 'redirect_uri', 'proxy_url']) {
    if (typeof body[field] !== 'string' || body[field].length === 0 || body[field].length > 8192) {
      throw new WorkerError('invalid_request', 400);
    }
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(body.email) || body.email.length > 320 || body.password.length > 1024) {
    throw new WorkerError('invalid_request', 400);
  }
  if (body.expected_email !== undefined && (typeof body.expected_email !== 'string'
    || body.expected_email.trim().toLowerCase() !== body.email.trim().toLowerCase())) {
    throw new WorkerError('identity_mismatch', 400);
  }
  if (body.workspace_id !== undefined && (typeof body.workspace_id !== 'string' || body.workspace_id.length > 128)) {
    throw new WorkerError('invalid_request', 400);
  }
  const url = parseURL(body.auth_url);
  const q = url.searchParams;
  const single = ['client_id', 'redirect_uri', 'state', 'code_challenge', 'code_challenge_method', 'response_type'];
  if (url.origin !== 'https://auth.openai.com' || url.pathname !== '/oauth/authorize' || url.username || url.password || url.hash
    || body.redirect_uri !== REDIRECT_URI || single.some(key => q.getAll(key).length !== 1)
    || q.get('redirect_uri') !== REDIRECT_URI || q.get('client_id') !== CLIENT_ID
    || q.get('response_type') !== 'code' || q.get('code_challenge_method') !== 'S256'
    || !/^[A-Za-z0-9_-]{43}$/.test(q.get('code_challenge') || '')
    || !/^[A-Za-z0-9_-]{16,256}$/.test(q.get('state') || '')) {
    throw new WorkerError('invalid_request', 400);
  }
  const key = decodeSecret(body.totp_secret);
  key.fill(0);
  const proxy = parseProxy(body.proxy_url);
  return { ...body, proxy, state: q.get('state') };
}

export function parseCallback(value, state) {
  const url = parseURL(value);
  if (`${url.origin}${url.pathname}` !== REDIRECT_URI || url.username || url.password || url.hash) {
    throw new WorkerError('invalid_callback');
  }
  const q = url.searchParams;
  if (q.getAll('state').length !== 1 || q.get('state') !== state) throw new WorkerError('state_mismatch');
  if (q.has('error')) throw new WorkerError('oauth_error');
  if (q.getAll('code').length !== 1 || !q.get('code') || q.get('code').length > 4096) {
    throw new WorkerError('invalid_callback');
  }
  return { code: q.get('code'), state };
}
