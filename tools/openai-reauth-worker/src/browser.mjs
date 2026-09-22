import { setTimeout as sleep } from 'node:timers/promises';
import { WorkerError } from './errors.mjs';
import { isAllowedResource, isCredentialOrigin, parseCallback, REDIRECT_URI } from './validation.mjs';
import { totp } from './totp.mjs';

const BUTTON_NEXT = /^(continue|next|sign in|log in|verify|submit|继续|下一步|登录|验证|提交)$/i;
const BUTTON_PASSWORD = /^(use (a |your )?password( instead)?|sign in with (a |your )?password|使用密码(登录)?|改用密码(登录)?)$/i;
const AUTHENTICATOR = /authenticator|authentication app|two.factor|two.step|身份验证器|身份验证应用|认证器|两步验证|双重验证/i;

export function classifyPage(text, url = '') {
  if (/account (has been |is )?(disabled|deactivated|suspended)|账户.{0,6}(停用|禁用|封禁)|账号.{0,6}(停用|禁用|封禁)/i.test(text)) return 'account_disabled';
  if (/incorrect (email|password|code)|wrong password|invalid (password|code)|密码.{0,4}(不正确|错误)|验证码.{0,4}(不正确|错误)/i.test(text)) return 'credentials_rejected';
  if (/too many (requests|attempts)|try again later|请求过多|尝试次数过多|稍后再试/i.test(text)) return 'rate_limited';
  if (/verify (that )?you are human|checking your browser|complete the captcha|验证您是人类|验证你是人类|安全检查/i.test(text)) return 'captcha_required';
  if (/check your (phone|device)|approve.{0,30}(device|phone)|verify with your passkey|insert your security key|检查.{0,10}(手机|设备)|使用.{0,5}(通行密钥|安全密钥).{0,5}验证/i.test(text)) return 'device_verification_required';
  if (/sent.{0,50}(email|inbox)|check your (email|inbox)|email.{0,30}(verification code|one.time)|已.{0,10}(发送|发送至).{0,20}(邮箱|邮件)|检查.{0,10}(邮箱|邮件)/i.test(text)
    || /\/email-(otp|verification)/i.test(url)) return 'email_verification_required';
  return null;
}

async function visible(locator) {
  const matches = await locator.all();
  for (const match of matches) if (await match.isVisible()) return match;
  return null;
}

async function ensureFormOrigin(field, page) {
  if (!isCredentialOrigin(page.url())) throw new WorkerError('untrusted_login_origin');
  const action = await field.evaluate(element => element.form?.action || location.href);
  if (!isCredentialOrigin(action)) throw new WorkerError('untrusted_login_origin');
}

async function submit(page, field) {
  await ensureFormOrigin(field, page);
  const form = field.locator('xpath=ancestor::form');
  const scope = await form.count() ? form : page;
  const button = await visible(scope.getByRole('button', { name: BUTTON_NEXT }));
  if (button) await button.click();
  else await field.press('Enter');
}

// This is a deliberately conservative UI adapter. Unknown/challenge screens stop
// instead of repeatedly trying credentials or attempting challenge bypasses.
export async function driveLogin(page, input, { signal, pollMs = 300, now = Date.now } = {}) {
  const completed = new Set();
  let unknownSince = now();
  while (!signal?.aborted) {
    const url = page.url();
    if (url.startsWith(REDIRECT_URI)) return;
    if (url === 'about:blank') { await sleep(pollMs, undefined, { signal }); continue; }
    if (!isCredentialOrigin(url)) throw new WorkerError('untrusted_login_origin');
    let text;
    try { text = await page.locator('body').innerText({ timeout: 1500 }); }
    catch { await sleep(pollMs, undefined, { signal }); continue; }

    const passwordSwitch = await visible(page.getByRole('button', { name: BUTTON_PASSWORD }))
      || await visible(page.getByRole('link', { name: BUTTON_PASSWORD }));
    if (passwordSwitch && !completed.has('password_switch')) {
      completed.add('password_switch');
      await passwordSwitch.click();
      unknownSince = now();
      continue;
    }
    const problem = classifyPage(text, url);
    if (problem) throw new WorkerError(problem);
    if (await visible(page.locator('iframe[src*="captcha"], iframe[src*="challenges.cloudflare.com"]'))) {
      throw new WorkerError('captcha_required');
    }

    const password = await visible(page.locator('input[type="password"]'));
    const email = await visible(page.locator('input[type="email"], input[autocomplete="username"], input[name="email"]'));
    const code = await visible(page.locator('input[autocomplete="one-time-code"], input[name="code"], input[name="otp"], input[name="totp"]'));
    const isTOTP = AUTHENTICATOR.test(text) || /\/mfa\/(totp|otp)|\/totp(?:\/|$)/i.test(url);

    if (password && !completed.has('password')) {
      await ensureFormOrigin(password, page);
      // Some forms display both fields on the same page.
      if (email) { await ensureFormOrigin(email, page); await email.fill(input.email); completed.add('email'); }
      await password.fill(input.password);
      completed.add('password');
      await submit(page, password);
      unknownSince = now();
    } else if (email && !completed.has('email')) {
      await ensureFormOrigin(email, page);
      await email.fill(input.email);
      completed.add('email');
      await submit(page, email);
      unknownSince = now();
    } else if (isTOTP && !completed.has('totp')) {
      // Avoid sending a code that is about to expire while the proxy is slow.
      const remaining = 30_000 - (now() % 30_000);
      if (remaining < 5000) { await sleep(remaining + 100, undefined, { signal }); continue; }
      const value = totp(input.totp_secret, now());
      if (code) {
        await ensureFormOrigin(code, page);
        await code.fill(value);
        completed.add('totp');
        await submit(page, code);
      } else {
        const digits = page.locator('input[maxlength="1"]:visible');
        if (await digits.count() !== 6) throw new WorkerError('login_flow_unsupported');
        for (let index = 0; index < 6; index++) {
          await ensureFormOrigin(digits.nth(index), page);
          await digits.nth(index).fill(value[index]);
        }
        completed.add('totp');
        await submit(page, digits.nth(5));
      }
      unknownSince = now();
    } else if (code && !isTOTP) {
      throw new WorkerError('email_verification_required');
    } else if (/select.{0,15}workspace|choose.{0,15}workspace|选择.{0,6}(工作区|工作空间)/i.test(text)) {
      if (!input.workspace_id) throw new WorkerError('workspace_selection_required');
      if (!completed.has('workspace')) {
        const select = await visible(page.locator('select'));
        if (!select) throw new WorkerError('workspace_selection_required');
        const values = await select.locator('option').evaluateAll(options => options.map(option => option.value));
        if (!values.includes(input.workspace_id)) throw new WorkerError('workspace_selection_required');
        await select.selectOption(input.workspace_id);
        completed.add('workspace');
        await submit(page, select);
        unknownSince = now();
      }
    } else if (!email && !password && !code && /codex/i.test(text) && /authorize|authorization|sign in to|continue to|access your|授权|登录|访问/i.test(text)
      && !completed.has('consent')) {
      const button = await visible(page.getByRole('button', { name: /^(allow|authorize|continue|允许|授权|继续)$/i }));
      if (button) {
        completed.add('consent');
        await button.click();
        unknownSince = now();
      }
    }
    if (now() - unknownSince > 20_000) throw new WorkerError('login_flow_unsupported');
    await sleep(pollMs, undefined, { signal });
  }
  throw new WorkerError('cancelled', 499);
}

export async function loginWithBrowser(input, { signal, timeoutMs = 180_000, chromium: chromiumOverride, launchOptions = {} } = {}) {
  const chromium = chromiumOverride || (await import('playwright')).chromium;
  let browser;
  const controller = new AbortController();
  const abort = () => controller.abort(new WorkerError('cancelled', 499));
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  const timer = setTimeout(() => controller.abort(new WorkerError('login_timeout', 504)), timeoutMs);
  const close = () => { if (browser) void browser.close().catch(() => {}); };
  controller.signal.addEventListener('abort', close, { once: true });
  const abortPromise = new Promise((_, reject) => {
    const rejectAbort = () => reject(controller.signal.reason);
    controller.signal.addEventListener('abort', rejectAbort, { once: true });
    if (controller.signal.aborted) rejectAbort();
  });
  // Mark handled even if launching the browser rejects before Promise.race below.
  void abortPromise.catch(() => {});
  try {
    if (controller.signal.aborted) throw controller.signal.reason;
    const env = { ...process.env };
    delete env.DEBUG;
    delete env.PWDEBUG;
    try {
      browser = await chromium.launch({
        ...launchOptions,
        headless: true,
        proxy: input.proxy,
        env,
        timeout: Math.min(timeoutMs, 30_000),
        args: ['--disable-quic', '--disable-background-networking', '--force-webrtc-ip-handling-policy=disable_non_proxied_udp', '--proxy-bypass-list=<-loopback>'],
      });
    } catch { throw new WorkerError('browser_unavailable', 503); }
    if (controller.signal.aborted) throw controller.signal.reason;
    const context = await browser.newContext({
      locale: 'en-US', serviceWorkers: 'block', acceptDownloads: false,
      permissions: [], viewport: { width: 1280, height: 900 },
    });
    context.setDefaultTimeout(5000);
    const page = await context.newPage();
    page.on('dialog', dialog => { void dialog.dismiss().catch(() => {}); });
    context.on('page', popup => { if (popup !== page) void popup.close().catch(() => {}); });
    let resolveCallback, rejectCallback;
    const callback = new Promise((resolve, reject) => { resolveCallback = resolve; rejectCallback = reject; });
    void callback.catch(() => {});
    await context.route('**/*', async route => {
      const request = route.request();
      const url = request.url();
      if (url.startsWith('http://localhost:1455/')) {
        try {
          if (request.frame() !== page.mainFrame() || !request.isNavigationRequest()) throw new WorkerError('invalid_callback');
          const result = parseCallback(url, input.state);
          await route.fulfill({ status: 200, contentType: 'text/plain', body: 'Authorization complete.' });
          resolveCallback(result);
        } catch (error) { rejectCallback(error); await route.abort().catch(() => {}); }
        return;
      }
      if (!isAllowedResource(url) || (request.isNavigationRequest() && !isCredentialOrigin(url))
        || (!['GET', 'HEAD', 'OPTIONS'].includes(request.method()) && !isCredentialOrigin(url))) {
        if (request.isNavigationRequest() && request.frame() === page.mainFrame()) rejectCallback(new WorkerError('untrusted_login_origin'));
        await route.abort();
        return;
      }
      await route.continue();
    });
    page.on('requestfailed', request => {
      if (/ERR_(PROXY|TUNNEL|SOCKS)|ERR_CONNECTION_(REFUSED|RESET|CLOSED)|ERR_NAME_NOT_RESOLVED/i.test(request.failure()?.errorText || '')) {
        rejectCallback(new WorkerError('proxy_unavailable', 502));
      }
    });
    // Callback can happen during the first navigation (for an already-authorized
    // upstream session); handlers are installed before any network request.
    const work = (async () => {
      try { await page.goto(input.auth_url, { waitUntil: 'domcontentloaded', timeout: 60_000 }); }
      catch (error) {
        if (/ERR_(PROXY|TUNNEL|SOCKS)|ERR_CONNECTION_|ERR_NAME_NOT_RESOLVED/i.test(String(error))) throw new WorkerError('proxy_unavailable', 502);
        throw new WorkerError('login_navigation_failed', 502);
      }
      await driveLogin(page, input, { signal: controller.signal });
      return callback;
    })();
    return await Promise.race([callback, work, abortPromise]);
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener('abort', abort);
    controller.abort(new WorkerError('cancelled', 499));
    if (browser) await browser.close().catch(() => {});
  }
}
