import test from 'node:test';
import assert from 'node:assert/strict';
import { decodeSecret, totp } from '../src/totp.mjs';
import { validateLogin, parseProxy, parseCallback, isCredentialOrigin, isAllowedResource, REDIRECT_URI } from '../src/validation.mjs';
import { classifyPage } from '../src/browser.mjs';
import { validInput } from './fixture.mjs';

test('RFC 6238 published SHA1 test vectors', () => {
  for (const [seconds, code] of [[59, '94287082'], [1111111109, '07081804'], [1111111111, '14050471'],
    [1234567890, '89005924'], [2000000000, '69279037'], [20000000000, '65353130']]) {
    assert.equal(totp(validInput().totp_secret, seconds * 1000, 8), code);
  }
  assert.equal(totp(`otpauth://totp/test?secret=${validInput().totp_secret}`, 59_000), '287082');
  assert.throws(() => decodeSecret('otpauth://hotp/test?secret=GEZDGNBVGY3TQOJQ'), { code: 'unsupported_totp' });
  assert.throws(() => decodeSecret('123456'), { code: 'invalid_request' });
});

test('official authorization endpoint, fixed client, PKCE, and unique state are required', () => {
  assert.equal(validateLogin(validInput()).state, 'fixture-state-with-enough-entropy');
  for (const mutate of [
    value => { value.auth_url = value.auth_url.replace('auth.openai.com', 'auth.openai.com.evil.test'); },
    value => { value.auth_url = value.auth_url.replace('https:', 'http:'); },
    value => { value.auth_url += '&state=another-state-value'; },
    value => { value.auth_url = value.auth_url.replace('S256', 'plain'); },
    value => { value.redirect_uri = 'http://127.0.0.1:1455/auth/callback'; },
    value => { value.auth_url = value.auth_url.replace('app_EMoamEEZ73f0CkXaXp7hrann', 'another-client'); },
  ]) {
    const input = validInput(); mutate(input);
    assert.throws(() => validateLogin(input), { code: 'invalid_request' });
  }
  assert.throws(() => validateLogin({ ...validInput(), expected_email: 'other@example.com' }), { code: 'identity_mismatch' });
});

test('callback cannot change origin, path, state or repeat code', () => {
  assert.deepEqual(parseCallback(`${REDIRECT_URI}?code=fixture&state=expected`, 'expected'), { code: 'fixture', state: 'expected' });
  assert.throws(() => parseCallback(`${REDIRECT_URI}?code=fixture&state=wrong`, 'expected'), { code: 'state_mismatch' });
  assert.throws(() => parseCallback(`${REDIRECT_URI}?code=a&code=b&state=expected`, 'expected'), { code: 'invalid_callback' });
  assert.throws(() => parseCallback('https://evil.test/auth/callback?code=a&state=expected', 'expected'), { code: 'invalid_callback' });
  assert.throws(() => parseCallback(`${REDIRECT_URI}?error=access_denied&state=expected`, 'expected'), { code: 'oauth_error' });
});

test('proxy is mandatory, supports HTTP authentication and refuses authenticated SOCKS', () => {
  assert.deepEqual(parseProxy('http://user:p%40ss@localhost:3128'), { server: 'http://localhost:3128', username: 'user', password: 'p@ss', bypass: '<-loopback>' });
  assert.equal(parseProxy('socks5h://127.0.0.1:1080').server, 'socks5://127.0.0.1:1080');
  assert.throws(() => parseProxy(''), { code: 'proxy_required' });
  assert.throws(() => parseProxy('socks5://user:password@localhost:1080'), { code: 'unsupported_proxy' });
  assert.throws(() => parseProxy('http://localhost:3128/path'), { code: 'unsupported_proxy' });
  assert.throws(() => parseProxy('file:///tmp/proxy'), { code: 'unsupported_proxy' });
});

test('credential destinations and resources have independent strict allowlists', () => {
  assert.equal(isCredentialOrigin('https://auth.openai.com/log-in'), true);
  assert.equal(isCredentialOrigin('https://chatgpt.com/auth/login'), true);
  for (const url of ['http://auth.openai.com', 'https://auth.openai.com.evil.test', 'https://cdn.oaistatic.com', 'https://auth.openai.com:444', 'https://user@auth.openai.com']) {
    assert.equal(isCredentialOrigin(url), false);
  }
  assert.equal(isAllowedResource('https://cdn.oaistatic.com/file.js'), true);
  assert.equal(isAllowedResource('http://localhost:8888/secrets'), false);
});

test('extra challenges and account failures stop with fixed safe error codes', () => {
  for (const [text, code] of [
    ['Your account has been deactivated', 'account_disabled'], ['Incorrect password', 'credentials_rejected'],
    ['Check your email for a code', 'email_verification_required'], ['Check your phone', 'device_verification_required'],
    ['Verify you are human', 'captcha_required'], ['Too many attempts', 'rate_limited'],
  ]) assert.equal(classifyPage(text), code);
  assert.equal(classifyPage('Enter the code from your authenticator app'), null);
});
