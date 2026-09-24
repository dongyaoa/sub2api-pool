import { afterEach, describe, expect, it, vi } from 'vitest'
import { JSDOM } from 'jsdom'
import { intelligencePreviewRuntime } from './intelligencePreviewRuntime'

const openDocuments: JSDOM[] = []
afterEach(() => {
  for (const dom of openDocuments.splice(0)) {
    dom.window.dispatchEvent(new dom.window.Event('pagehide'))
    dom.window.close()
  }
})

function setup(autoplay: boolean, body = '<main><svg></svg><canvas></canvas></main>') {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${body}</body></html>`, { runScripts: 'outside-only', pretendToBeVisual: true })
  openDocuments.push(dom)
  const window = dom.window
  const controller = { postMessage: vi.fn() }
  Object.defineProperty(window, 'parent', { value: controller, configurable: true })
  Object.defineProperty(window.document, 'readyState', { value: 'complete', configurable: true })
  let clock = 0, handle = 0
  const frames = new Map<number, FrameRequestCallback>()
  const timers = new Map<number, { callback: () => void, at: number }>()
  window.requestAnimationFrame = callback => { frames.set(++handle, callback); return handle }
  window.cancelAnimationFrame = id => { frames.delete(id) }
  window.setTimeout = ((callback: () => void, delay = 0) => { timers.set(++handle, { callback, at: clock + delay }); return handle }) as typeof window.setTimeout
  window.clearTimeout = id => { timers.delete(id) }
  vi.spyOn(window.performance, 'now').mockImplementation(() => clock)
  const paused = vi.fn(), unpaused = vi.fn()
  const animations: Array<{ playState: string, pending: boolean, pause: ReturnType<typeof vi.fn>, play: ReturnType<typeof vi.fn> }> = []
  window.Element.prototype.animate = vi.fn(() => {
    const animation = { playState: 'running', pending: false, pause: vi.fn(), play: vi.fn() }
    animation.pause.mockImplementation(() => { animation.playState = 'paused' })
    animation.play.mockImplementation(() => { animation.playState = 'running' })
    animations.push(animation)
    return animation as unknown as Animation
  })
  window.document.getAnimations = () => animations as unknown as Animation[]
  for (const svg of window.document.querySelectorAll('svg')) {
    Object.assign(svg, { pauseAnimations: paused, unpauseAnimations: unpaused })
  }
  const observers: Array<{ callback: ResizeObserverCallback, observe: ReturnType<typeof vi.fn>, disconnect: ReturnType<typeof vi.fn> }> = []
  window.ResizeObserver = class {
    observe = vi.fn()
    disconnect = vi.fn()
    unobserve = vi.fn()
    constructor(public callback: ResizeObserverCallback) { observers.push(this) }
  }
  const frame = () => {
    clock += 16
    const callbacks = [...frames.values()]
    frames.clear()
    for (const callback of callbacks) callback(clock)
  }
  const advance = (milliseconds: number) => {
    const end = clock + milliseconds
    let count = 0
    while (true) {
      const next = [...timers].filter(([, timer]) => timer.at <= end).sort((a, b) => a[1].at - b[1].at)[0]
      if (!next) break
      if (++count > 100) throw new Error('Timer did not settle')
      clock = next[1].at
      timers.delete(next[0])
      next[1].callback()
    }
    clock = end
  }
  const playback = (playing: unknown, source: unknown = controller) => window.dispatchEvent(new window.MessageEvent('message', { data: { type: 'intelligence-preview-playback', playing }, source }))
  window.eval(intelligencePreviewRuntime(autoplay))
  return { window, controller, frame, advance, playback, paused, unpaused, observers, frames, timers, animations }
}

describe('intelligence preview runtime', () => {
  it('draws a thumbnail first frame, then pauses and resumes RAF, CSS and SVG only for its parent', () => {
    const runtime = setup(false)
    const { window, frame, advance, playback, controller, paused, unpaused } = runtime
    const draw = vi.fn()
    const render = (timestamp: number) => { draw(timestamp); window.requestAnimationFrame(render) }
    window.requestAnimationFrame(render)
    frame()
    expect(draw).toHaveBeenCalledOnce()
    advance(120)
    expect(window.document.documentElement.hasAttribute('data-intelligence-preview-paused')).toBe(true)
    expect(window.document.head.textContent).toContain('animation-play-state:paused!important')
    expect(paused).toHaveBeenCalled()
    frame()
    expect(draw).toHaveBeenCalledOnce()
    playback(true, { postMessage: vi.fn() })
    playback('true')
    frame()
    expect(draw).toHaveBeenCalledOnce()
    playback(true)
    frame()
    expect(draw).toHaveBeenCalledTimes(2)
    expect(window.document.documentElement.hasAttribute('data-intelligence-preview-paused')).toBe(false)
    expect(unpaused).toHaveBeenCalled()
    expect(controller.postMessage).toHaveBeenCalledWith({ type: 'intelligence-preview-ready' }, '*')
  })

  it('autoplays enlarged previews and honors cancellation within the same RAF batch', () => {
    const { window, frame, advance } = setup(true)
    const canceled = vi.fn(), running = vi.fn()
    let canceledHandle = 0
    window.requestAnimationFrame(() => { running(); window.cancelAnimationFrame(canceledHandle) })
    canceledHandle = window.requestAnimationFrame(canceled)
    advance(200)
    frame()
    expect(running).toHaveBeenCalledOnce()
    expect(canceled).not.toHaveBeenCalled()
    expect(window.document.documentElement.hasAttribute('data-intelligence-preview-paused')).toBe(false)
  })

  it('preserves remaining timer delays across a pause and keeps clearTimeout/clearInterval interchangeable', () => {
    const { window, advance, playback } = setup(false)
    const later = vi.fn(), repeating = vi.fn(), canceled = vi.fn()
    window.setTimeout(later, 200, 'canvas')
    const interval = window.setInterval(repeating, 50)
    const timeout = window.setTimeout(canceled, 160)
    window.clearInterval(timeout)
    advance(120)
    expect(repeating).toHaveBeenCalledTimes(2)
    advance(1000)
    expect(repeating).toHaveBeenCalledTimes(2)
    expect(later).not.toHaveBeenCalled()
    playback(true)
    advance(30)
    expect(repeating).toHaveBeenCalledTimes(3)
    window.clearTimeout(interval)
    advance(49)
    expect(later).not.toHaveBeenCalled()
    advance(1)
    expect(later).toHaveBeenCalledWith('canvas')
    expect(canceled).not.toHaveBeenCalled()
    expect(repeating).toHaveBeenCalledTimes(3)
  })

  it('pauses active Web Animations and animations created later without resuming originally paused ones', () => {
    const { window, advance, playback, animations } = setup(false)
    const svg = window.document.querySelector('svg')!
    svg.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 1000 })
    svg.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 1000 }).pause()
    advance(120)
    expect(animations[0].pause).toHaveBeenCalledOnce()
    svg.animate([{ transform: 'rotate(0)' }, { transform: 'rotate(1turn)' }], { duration: 1000 })
    expect(animations[2].pause).toHaveBeenCalledOnce()
    playback(true)
    expect(animations[0].play).toHaveBeenCalledOnce()
    expect(animations[2].play).toHaveBeenCalledOnce()
    expect(animations[1].play).not.toHaveBeenCalled()
  })

  it('contains a tall document without changing its logical viewport or accumulating transforms', () => {
    const { window, frame, playback } = setup(false, '<header>Title</header><svg></svg>')
    const { document } = window
    Object.defineProperties(document.documentElement, { scrollWidth: { value: 960 }, scrollHeight: { value: 1040 } })
    Object.defineProperties(document.body, { scrollWidth: { value: 960 }, scrollHeight: { value: 1040 } })
    const rect = vi.spyOn(document.body, 'getBoundingClientRect').mockImplementation(() => {
      expect(document.documentElement.style.transform).toBe('none')
      return { left: 0, top: 0, right: 960, bottom: 1040, width: 960, height: 1040, x: 0, y: 0, toJSON: () => ({}) }
    })
    frame()
    const expectedScale = 600 / 1040
    expect(document.documentElement.style.transform).toBe(`translate(${(960 - 960 * expectedScale) / 2}px, 0px) scale(${expectedScale})`)
    const initial = document.documentElement.style.transform
    playback(true)
    frame()
    expect(document.documentElement.style.transform).toBe(initial)
    expect(rect).toHaveBeenCalledTimes(2)
    expect(document.documentElement.style.height).toBe('')
    expect(document.body.style.height).toBe('')
    expect(document.documentElement.style.overflow).toBe('visible')
    expect(document.body.style.overflow).toBe('visible')
  })

  it('includes negative content bounds and ignores invalid dimensions', () => {
    const { window, frame } = setup(true, '<svg></svg>')
    const { document } = window
    Object.defineProperties(document.documentElement, { scrollWidth: { value: Infinity }, scrollHeight: { value: NaN } })
    vi.spyOn(document.body.firstElementChild!, 'getBoundingClientRect').mockReturnValue({ left: -120, top: -50, right: 1080, bottom: 650, width: 1200, height: 700, x: -120, y: -50, toJSON: () => ({}) })
    frame()
    const scale = 0.8
    expect(document.documentElement.style.transform).toBe(`translate(96px, 60px) scale(${scale})`)
    expect(document.documentElement.style.transform).not.toMatch(/NaN|Infinity/)
  })

  it('refits document changes without observing animation attribute changes', async () => {
    const { window, frame, observers } = setup(false)
    frame()
    const initialObserver = observers[0]
    expect(initialObserver.observe.mock.calls.some(call => call[0] === window.document.body)).toBe(true)
    const content = window.document.createElement('section')
    content.textContent = 'New caption'
    window.document.body.append(content)
    await new Promise<void>(resolve => queueMicrotask(resolve))
    expect(initialObserver.observe.mock.calls.some(call => call[0] === content)).toBe(true)
    expect(initialObserver.disconnect).toHaveBeenCalledTimes(2)
    window.document.querySelector('svg')!.setAttribute('transform', 'rotate(10)')
    await Promise.resolve()
    expect(initialObserver.disconnect).toHaveBeenCalledTimes(2)
    frame()
  })
})
