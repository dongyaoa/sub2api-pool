import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { createWorkerServer } from '../src/server.mjs';
import { validInput } from './fixture.mjs';

const token = 'fixture-worker-token-with-at-least-32-characters';
async function start(t, login) {
  const server = createWorkerServer({ token, login });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  t.after(() => { server.cancelJobs(); server.closeAllConnections(); server.close(); });
  return `http://127.0.0.1:${server.address().port}`;
}
function post(base, payload = validInput(), headers = {}) {
  return fetch(`${base}/login`, { method: 'POST', headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json', ...headers }, body: JSON.stringify(payload) });
}

test('server requires an explicit strong worker token at startup', () => {
  assert.throws(() => createWorkerServer(), { code: 'invalid_worker_token' });
  assert.throws(() => createWorkerServer({ token: 'weak' }), { code: 'invalid_worker_token' });
});

test('health returns no credentials; login requires bearer authentication', async t => {
  let called = false;
  const base = await start(t, async () => { called = true; });
  const health = await fetch(`${base}/health`);
  assert.deepEqual(await health.json(), { status: 'ok' });
  const response = await post(base, validInput(), { authorization: 'Bearer invalid' });
  assert.equal(response.status, 401);
  assert.deepEqual(await response.json(), { error_code: 'unauthorized' });
  assert.equal(called, false);
});

test('successful login receives normalized proxy and returns only code/state', async t => {
  const base = await start(t, async input => {
    assert.equal(input.proxy.server, 'http://127.0.0.1:19090');
    return { code: 'fixture-auth-code', state: input.state };
  });
  const response = await post(base);
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { code: 'fixture-auth-code', state: 'fixture-state-with-enough-entropy' });
  assert.equal(response.headers.get('cache-control'), 'no-store');
});

test('untrusted requests fail before browser launch, errors never include secrets', async t => {
  let calls = 0;
  const base = await start(t, async () => { calls++; throw new Error('fixture-password and browser stack'); });
  const invalid = await post(base, { ...validInput(), proxy_url: '' });
  assert.equal(invalid.status, 400);
  assert.equal(calls, 0);
  const response = await post(base);
  assert.equal(response.status, 502);
  assert.deepEqual(await response.json(), { error_code: 'login_failed' });
});

test('concurrent login is bounded and client disconnect cancels current job', async t => {
  let entered, aborted;
  const entering = new Promise(resolve => { entered = resolve; });
  const aborting = new Promise(resolve => { aborted = resolve; });
  const base = await start(t, async (_input, { signal }) => {
    entered();
    return new Promise((_, reject) => signal.addEventListener('abort', () => { aborted(); reject(new Error('cancelled')); }, { once: true }));
  });
  const request = http.request(`${base}/login`, { method: 'POST', headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' } });
  request.on('error', () => {});
  request.end(JSON.stringify(validInput()));
  await entering;
  const busy = await post(base);
  assert.equal(busy.status, 429);
  assert.deepEqual(await busy.json(), { error_code: 'busy' });
  request.destroy();
  await aborting;
});
