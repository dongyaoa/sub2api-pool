import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { chromium } from 'playwright';
import { loginWithBrowser } from '../src/browser.mjs';
import { validateLogin, REDIRECT_URI } from '../src/validation.mjs';
import { totp } from '../src/totp.mjs';
import { validInput } from './fixture.mjs';

const launchOptions = process.env.PLAYWRIGHT_TEST_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_TEST_EXECUTABLE_PATH } : {};

// Fixtures replace only the network response. The actual Chromium DOM, form
// submission, adapter, callback interception and browser teardown are exercised.
function fixtureChromium(fixture, observations) {
  return { launch: async options => {
    observations.launchOptions = options;
    const browser = await chromium.launch(options);
    observations.browser = browser;
    const newContext = browser.newContext.bind(browser);
    browser.newContext = async options => {
      const context = await newContext(options);
      const addRoute = context.route.bind(context);
      context.route = (pattern, handler) => addRoute(pattern, route => handler(new Proxy(route, {
        get(target, property) {
          if (property === 'continue') return () => fixture(target);
          const value = Reflect.get(target, property);
          return typeof value === 'function' ? value.bind(target) : value;
        },
      })));
      return context;
    };
    return browser;
  } };
}

async function html(route, body) {
  await route.fulfill({ status: 200, contentType: 'text/html', body: `<!doctype html><html><body>${body}</body></html>` });
}
function form(action, body) { return `<form method="post" action="${action}">${body}<button type="submit">Continue</button></form>`; }

test('headless email + password + TOTP + consent completes OAuth and closes browser', { timeout: 45_000 }, async () => {
  const input = validateLogin(validInput());
  const observed = { stages: [] };
  const fixture = async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const data = new URLSearchParams(request.postData() || '');
    observed.stages.push(path);
    if (path === '/oauth/authorize') return html(route, form('/log-in/password', '<label>Email address<input name="email" type="email"></label>'));
    if (path === '/log-in/password') {
      assert.equal(data.get('email'), input.email);
      return html(route, form('/mfa/totp', '<label>Password<input name="password" type="password"></label>'));
    }
    if (path === '/mfa/totp') {
      assert.equal(data.get('password'), input.password);
      return html(route, form('/oauth/consent', '<h1>Enter your authenticator app code</h1><input name="code" autocomplete="one-time-code">'));
    }
    if (path === '/oauth/consent') {
      assert.ok([totp(input.totp_secret), totp(input.totp_secret, Date.now() - 30_000)].includes(data.get('code')));
      return html(route, form(`${REDIRECT_URI}?state=${input.state}&code=fixture-authorization-code`, '<h1>Sign in to Codex</h1>'));
    }
    return html(route, 'Unexpected request');
  };
  const result = await loginWithBrowser(input, { chromium: fixtureChromium(fixture, observed), launchOptions, timeoutMs: 40_000 });
  assert.deepEqual(result, { code: 'fixture-authorization-code', state: input.state });
  assert.equal(observed.launchOptions.headless, true);
  assert.deepEqual(observed.launchOptions.proxy, input.proxy);
  assert.equal(observed.browser.isConnected(), false);
  assert.deepEqual(observed.stages, ['/oauth/authorize', '/log-in/password', '/mfa/totp', '/oauth/consent']);
});

test('email verification stops without submitting the TOTP secret or code', { timeout: 20_000 }, async () => {
  const observed = {};
  const fixture = route => html(route, form('/email-otp', '<h1>Check your email</h1><input autocomplete="one-time-code" name="code">'));
  await assert.rejects(loginWithBrowser(validateLogin(validInput()), { chromium: fixtureChromium(fixture, observed), launchOptions }), { code: 'email_verification_required' });
  assert.equal(observed.browser.isConnected(), false);
});

test('malicious form action is rejected before entering password', { timeout: 20_000 }, async () => {
  const observed = {};
  const fixture = route => html(route, form('https://evil.test/collect', '<label>Password<input type="password"></label>'));
  await assert.rejects(loginWithBrowser(validateLogin(validInput()), { chromium: fixtureChromium(fixture, observed), launchOptions }), { code: 'untrusted_login_origin' });
  assert.equal(observed.browser.isConnected(), false);
});

test('callback validates state before delivering any code', { timeout: 20_000 }, async () => {
  const observed = {};
  const fixture = route => html(route, `<script>location.href=${JSON.stringify(`${REDIRECT_URI}?state=wrong&code=fixture`)}</script>`);
  await assert.rejects(loginWithBrowser(validateLogin(validInput()), { chromium: fixtureChromium(fixture, observed), launchOptions }), { code: 'state_mismatch' });
});

test('actual proxy failure is terminal and never falls back to a direct connection', { timeout: 20_000 }, async t => {
  const destinations = [];
  const proxy = http.createServer((_req, res) => { res.writeHead(502); res.end(); });
  proxy.on('connect', (request, socket) => {
    destinations.push(request.url);
    socket.end('HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n');
  });
  proxy.listen(0, '127.0.0.1');
  await once(proxy, 'listening');
  t.after(() => { proxy.closeAllConnections(); proxy.close(); });
  const input = validateLogin({ ...validInput(), proxy_url: `http://127.0.0.1:${proxy.address().port}` });
  await assert.rejects(loginWithBrowser(input, { launchOptions, timeoutMs: 12_000 }), { code: 'proxy_unavailable' });
  assert.ok(destinations.includes('auth.openai.com:443'));
});

test('deadline abort closes browser with no lingering login process', { timeout: 20_000 }, async () => {
  const observed = {};
  const fixture = route => html(route, '<h1>Loading</h1>');
  await assert.rejects(loginWithBrowser(validateLogin(validInput()), { chromium: fixtureChromium(fixture, observed), launchOptions, timeoutMs: 4000 }), { code: 'login_timeout' });
  assert.equal(observed.browser.isConnected(), false);
});
