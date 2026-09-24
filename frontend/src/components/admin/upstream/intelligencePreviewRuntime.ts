/**
 * Runs inside the generated artwork's opaque-origin iframe. The containing
 * iframe supplies the security boundary; these hooks only control playback and
 * fit the document into the fixed 960 × 600 artwork viewport. Thumbnails cover
 * their visible card area; detail views contain the complete document.
 */
export function intelligencePreviewRuntime(autoplay: boolean, fit: 'contain' | 'cover' = 'contain'): string {
  return `(() => {
  'use strict';
  const WIDTH = 960, HEIGHT = 600, MAX_EXTENT = 8192;
  const cover = ${JSON.stringify(fit === 'cover')};
  let visibleWidth = WIDTH, visibleHeight = HEIGHT;
  const nativeFrame = window.requestAnimationFrame.bind(window);
  const nativeCancelFrame = window.cancelAnimationFrame.bind(window);
  const nativeTimeout = window.setTimeout.bind(window);
  const nativeClearTimeout = window.clearTimeout.bind(window);
  const now = performance.now.bind(performance);
  const reportError = typeof window.reportError === 'function'
    ? window.reportError.bind(window)
    : error => nativeTimeout(() => { throw error; }, 0);
  const frameCallbacks = new Map(), timers = new Map(), pausedAnimations = new Set();
  let nextHandle = 1, pendingFrame = null, pendingFit = null;
  let playing = ${JSON.stringify(autoplay)}, warming = true, active = true, disposed = false;
  let pausedAt = 0, pausedDuration = 0, resizeObserver = null, mutationObserver = null;

  function scheduleFrames() {
    if (disposed || !active || pendingFrame !== null || !frameCallbacks.size) return;
    pendingFrame = nativeFrame(timestamp => {
      pendingFrame = null;
      if (!active) return;
      const callbacks = Array.from(frameCallbacks.entries());
      for (const [handle, callback] of callbacks) {
        if (!frameCallbacks.has(handle)) continue;
        frameCallbacks.delete(handle);
        try { callback(timestamp - pausedDuration); } catch (error) { reportError(error); }
      }
      scheduleFrames();
    });
  }
  window.requestAnimationFrame = callback => {
    if (typeof callback !== 'function') throw new TypeError('Animation callback must be a function');
    const handle = nextHandle++;
    frameCallbacks.set(handle, callback);
    scheduleFrames();
    return handle;
  };
  window.cancelAnimationFrame = handle => { frameCallbacks.delete(handle); };

  function armTimer(timer) {
    if (disposed || !active || timer.nativeHandle !== null || !timers.has(timer.handle)) return;
    timer.dueAt = now() + timer.remaining;
    timer.nativeHandle = nativeTimeout(() => {
      timer.nativeHandle = null;
      if (!active || !timers.has(timer.handle)) return;
      if (!timer.repeat) timers.delete(timer.handle);
      try { timer.callback.apply(window, timer.args); } catch (error) { reportError(error); }
      if (timer.repeat && timers.has(timer.handle)) {
        timer.remaining = timer.delay;
        armTimer(timer);
      }
    }, timer.remaining);
  }
  function addTimer(callback, delay, args, repeat) {
    // String callbacks require eval privileges, which the preview CSP denies.
    if (typeof callback !== 'function') return 0;
    const handle = nextHandle++;
    const milliseconds = Math.min(2147483647, Math.max(0, Number(delay) || 0));
    const timer = { handle, callback, args, repeat, delay: milliseconds,
      remaining: milliseconds, dueAt: 0, nativeHandle: null };
    timers.set(handle, timer);
    armTimer(timer);
    return handle;
  }
  window.setTimeout = (callback, delay, ...args) => addTimer(callback, delay, args, false);
  window.setInterval = (callback, delay, ...args) => addTimer(callback, delay, args, true);
  function clearTimer(handle) {
    const timer = timers.get(handle);
    if (!timer) return;
    if (timer.nativeHandle !== null) nativeClearTimeout(timer.nativeHandle);
    timers.delete(handle);
  }
  window.clearTimeout = clearTimer;
  window.clearInterval = clearTimer;

  function applyAnimationState() {
    const root = document.documentElement;
    if (!root) return;
    root.toggleAttribute('data-intelligence-preview-paused', !active);
    for (const svg of document.querySelectorAll('svg')) {
      const method = active ? 'unpauseAnimations' : 'pauseAnimations';
      if (typeof svg[method] === 'function') svg[method]();
    }
    if (active) {
      for (const animation of pausedAnimations) {
        if (animation.playState === 'paused') animation.play();
      }
      pausedAnimations.clear();
    } else if (typeof document.getAnimations === 'function') {
      for (const animation of document.getAnimations()) {
        if (animation.playState === 'running' || animation.pending) {
          pausedAnimations.add(animation);
          animation.pause();
        }
      }
    }
  }
  const nativeAnimate = window.Element.prototype.animate;
  if (typeof nativeAnimate === 'function') {
    window.Element.prototype.animate = function (...args) {
      const animation = nativeAnimate.apply(this, args);
      if (!active) { pausedAnimations.add(animation); animation.pause(); }
      return animation;
    };
  }
  function reconcilePlayback() {
    const nextActive = playing || warming;
    if (nextActive !== active) {
      active = nextActive;
      if (active) {
        pausedDuration += now() - pausedAt;
        for (const timer of timers.values()) armTimer(timer);
        scheduleFrames();
      } else {
        pausedAt = now();
        if (pendingFrame !== null) nativeCancelFrame(pendingFrame);
        pendingFrame = null;
        for (const timer of timers.values()) {
          if (timer.nativeHandle === null) continue;
          timer.remaining = Math.max(0, timer.dueAt - pausedAt);
          nativeClearTimeout(timer.nativeHandle);
          timer.nativeHandle = null;
        }
      }
    }
    applyAnimationState();
  }
  window.addEventListener('message', event => {
    if (disposed || event.source !== window.parent || !event.data) return;
    if (event.data.type === 'intelligence-preview-viewport') {
      const { width, height } = event.data;
      if (!cover || !Number.isFinite(width) || !Number.isFinite(height) ||
          width <= 0 || height <= 0 || width > WIDTH || height > HEIGHT) return;
      if (width !== visibleWidth || height !== visibleHeight) {
        visibleWidth = width; visibleHeight = height; scheduleFit();
      }
      return;
    }
    if (event.data.type !== 'intelligence-preview-playback' || typeof event.data.playing !== 'boolean') return;
    playing = event.data.playing;
    reconcilePlayback();
    scheduleFit();
  });

  function finite(value, fallback) {
    return Number.isFinite(value) ? Math.max(-MAX_EXTENT, Math.min(MAX_EXTENT, value)) : fallback;
  }
  function fitDocument() {
    pendingFit = null;
    const root = document.documentElement, body = document.body;
    if (!root || !body) return;
    // Read natural dimensions at the same fixed viewport every time. Changing
    // iframe height or CSS zoom would feed viewport-relative layouts back into
    // the next measurement and repeatedly enlarge or shrink the artwork.
    root.style.setProperty('transform', 'none', 'important');
    root.style.setProperty('transform-origin', '0 0', 'important');
    root.style.setProperty('overflow', 'visible', 'important');
    body.style.setProperty('overflow', 'visible', 'important');
    let left = 0, top = 0;
    let right = Math.max(WIDTH, finite(root.scrollWidth, WIDTH), finite(body.scrollWidth, WIDTH));
    let bottom = Math.max(HEIGHT, finite(root.scrollHeight, HEIGHT), finite(body.scrollHeight, HEIGHT));
    // Top-level bounds include centred oversized canvases/SVGs and margins;
    // animated descendants are deliberately excluded to avoid frame jitter.
    for (const element of [body, ...body.children]) {
      const rect = element.getBoundingClientRect();
      left = Math.min(left, finite(rect.left, 0));
      top = Math.min(top, finite(rect.top, 0));
      right = Math.max(right, finite(rect.right, WIDTH));
      bottom = Math.max(bottom, finite(rect.bottom, HEIGHT));
    }
    const width = Math.min(MAX_EXTENT, Math.max(WIDTH, right - left));
    const height = Math.min(MAX_EXTENT, Math.max(HEIGHT, bottom - top));
    const scale = cover
      ? Math.max(visibleWidth / width, visibleHeight / height)
      : Math.min(1, WIDTH / width, HEIGHT / height);
    const x = (WIDTH - width * scale) / 2 - left * scale;
    const y = (HEIGHT - height * scale) / 2 - top * scale;
    root.style.setProperty('transform', 'translate(' + x + 'px, ' + y + 'px) scale(' + scale + ')', 'important');
  }
  function scheduleFit() {
    if (!disposed && pendingFit === null) pendingFit = nativeFrame(fitDocument);
  }
  function observeLayout() {
    if (disposed || !resizeObserver || !document.body) return;
    resizeObserver.disconnect();
    resizeObserver.observe(document.body);
    for (const element of document.body.children) {
      if (!['SCRIPT', 'STYLE'].includes(element.tagName)) resizeObserver.observe(element);
    }
  }
  function initialize() {
    const style = document.createElement('style');
    style.textContent = '[data-intelligence-preview-paused], [data-intelligence-preview-paused] *, [data-intelligence-preview-paused] *::before, [data-intelligence-preview-paused] *::after{animation-play-state:paused!important}';
    document.head.append(style);
    if (typeof ResizeObserver === 'function') resizeObserver = new ResizeObserver(scheduleFit);
    observeLayout();
    mutationObserver = new MutationObserver(() => {
      if (disposed) return;
      observeLayout(); applyAnimationState(); scheduleFit();
    });
    mutationObserver.observe(document.documentElement, { childList: true, subtree: true, characterData: true });
    document.addEventListener('load', scheduleFit, true);
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(scheduleFit);
    scheduleFit();
    // Let self-contained canvas artwork draw its first frame before pausing a
    // thumbnail. This short warm-up also runs DOM-ready initialization timers.
    nativeTimeout(() => { if (!disposed) { warming = false; reconcilePlayback(); scheduleFit(); } }, 120);
    window.parent.postMessage({ type: 'intelligence-preview-ready' }, '*');
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', initialize, { once: true });
  else initialize();
  window.addEventListener('pagehide', () => {
    disposed = true;
    if (pendingFrame !== null) nativeCancelFrame(pendingFrame);
    if (pendingFit !== null) nativeCancelFrame(pendingFit);
    for (const timer of timers.values()) if (timer.nativeHandle !== null) nativeClearTimeout(timer.nativeHandle);
    frameCallbacks.clear(); timers.clear(); pausedAnimations.clear();
    if (resizeObserver) resizeObserver.disconnect();
    if (mutationObserver) mutationObserver.disconnect();
  }, { once: true });
})();`;
}
