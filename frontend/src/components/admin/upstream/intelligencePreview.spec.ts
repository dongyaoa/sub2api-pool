import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { JSDOM } from 'jsdom'
import { intelligenceMonitorAPI, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import { intelligencePreviewContent, intelligencePreviewDocument } from './intelligencePreview'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail: vi.fn() } }))
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

function parsePreview(value: string): Document {
  const shell = new DOMParser().parseFromString(value, 'text/html')
  return new DOMParser().parseFromString(shell.querySelector('iframe')?.getAttribute('srcdoc') || '', 'text/html')
}

describe('intelligence preview isolation', () => {
  it('removes active HTML, SVG navigation and remote resource surfaces', () => {
    const result = intelligencePreviewContent(`<!doctype html><html><head><meta http-equiv="refresh" content="0;url=https://evil.test"><link rel="stylesheet" href="https://evil.test/x.css"></head><body onload="fetch('/session')"><a href="https://evil.test" target="_top">go</a><form action="https://evil.test"><input formaction="https://evil.test"></form><iframe src="https://evil.test"></iframe><svg><use href="https://evil.test/icon.svg#x"/><image href="https://evil.test/a.png"/><animate attributeName="href" to="https://evil.test"/></svg><script>location='https://evil.test'</script></body></html>`)
    const doc = parsePreview(result.document)

    expect(result.scriptsDisabled).toBe(true)
    expect(doc.querySelector('meta[http-equiv="refresh"],link,iframe,form,a,base')).toBeNull()
    expect(doc.querySelector('[onload], [formaction], [target], [href^="http"], [src^="http"]')).toBeNull()
    expect(doc.querySelector('svg use')).not.toBeNull()
    expect(doc.querySelector('svg use')?.getAttribute('href')).toBeNull()
    expect(doc.querySelector('svg animate')).toBeNull()
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')).toContain("connect-src 'none'")
  })

  it('keeps inert SVG SMIL, local SVG references and inline CSS animations', () => {
    const result = intelligencePreviewContent('<html><head><style>body{background:skyblue}</style></head><body class="scene" style="margin:4px"><svg viewBox="0 0 10 10"><style>@keyframes pulse{to{opacity:0}}</style><circle id="wheel" cx="5" cy="5" r="2"><animate attributeName="opacity" values="1;0;1" dur="2s" repeatCount="indefinite"/></circle><use href="#wheel"/><g><animateTransform attributeName="transform" type="rotate" from="0 5 5" to="360 5 5" dur="1s" repeatCount="indefinite"/></g></svg></body></html>')
    const doc = parsePreview(result.document)

    expect(result.scriptsDisabled).toBe(false)
    expect(doc.querySelector('svg circle animate')).not.toBeNull()
    expect(doc.querySelector('svg style')?.textContent).toContain('@keyframes pulse')
    expect(doc.querySelector('svg animate')?.getAttribute('attributeName')).toBe('opacity')
    expect(doc.querySelector('svg use')?.getAttribute('href')).toBe('#wheel')
    expect(doc.querySelector('animateTransform')?.getAttribute('from')).toBe('0 5 5')
    expect(doc.body.className).toBe('scene')
    expect(doc.body.getAttribute('style')).toBe('margin:4px')
    expect(doc.head.textContent).toContain('background:skyblue')
  })

  it('blocks SMIL from restoring navigation or event attributes', () => {
    const doc = parsePreview(intelligencePreviewDocument('<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="https://evil.test" target="_blank"><text>Click</text><set attributeName="xlink:href" to="https://evil.test"/></a><g><set attributeName="onload" to="alert(1)"/><animate attributeName="opacity" values="1;0" dur="1s"/></g></svg>'))
    expect(doc.querySelector('a, set')).toBeNull()
    expect(doc.querySelector('text')?.textContent).toBe('Click')
    expect(doc.querySelector('animate')).not.toBeNull()
  })

  it('executes inline animation only inside two opaque sandboxes and preserves raw source', async () => {
    const raw = '<svg><script>window.top.location = "https://evil.test"</script><circle /></svg>'
    const run = Object.freeze({ id: 1, status: 'succeeded', html: raw }) as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { props: { run, large: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('allow-scripts')
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes('credentialless')).toBeDefined()
    const shell = new DOMParser().parseFromString(frame.attributes('srcdoc'), 'text/html')
    expect(shell.querySelector('iframe')?.getAttribute('sandbox')).toBe('allow-scripts')
    expect(shell.querySelector('meta')?.getAttribute('content')).toContain("frame-src 'none'")
    expect(Array.from(shell.querySelectorAll('script')).map(script => script.textContent).join('')).not.toContain('evil.test')
    expect(parsePreview(frame.attributes('srcdoc')).body.textContent).toContain('window.top.location')
    expect(wrapper.find('[role="note"]').exists()).toBe(false)
    expect(run.html).toBe(raw)
    wrapper.unmount()
  })

  it('fits the fixed detail canvas into large viewports and disconnects its resize observer', async () => {
    const observe = vi.fn(), disconnect = vi.fn()
    let resize: ResizeObserverCallback | undefined
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { resize = callback }
      observe = observe
      disconnect = disconnect
    })
    const run = { id: 2, status: 'succeeded', html: '<svg viewBox="0 0 960 600"><circle cx="480" cy="300" r="20"/></svg>' } as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { props: { run, large: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    const resizeTo = async (width: number, height: number) => {
      resize?.([{ target: wrapper.element, contentRect: { width, height } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
      await flushPromises()
    }

    expect(observe).toHaveBeenCalledWith(wrapper.element)
    expect(wrapper.classes()).toContain('aspect-[16/10]')
    expect(frame.style.width).toBe('960px')
    expect(frame.style.height).toBe('600px')
    expect(frame.getAttribute('scrolling')).toBe('no')
    await resizeTo(353, 220.625)
    expect(Number(frame.style.transform.match(/scale\((.+)\)/)?.[1])).toBeCloseTo(353 / 960)
    expect(parseFloat(frame.style.left)).toBeCloseTo(0)
    expect(parseFloat(frame.style.top)).toBeCloseTo(0)
    await resizeTo(320, 420)
    expect(Number(frame.style.transform.match(/scale\((.+)\)/)?.[1])).toBeCloseTo(1 / 3)
    expect(frame.style.top).toBe('110px')
    expect(frame.style.left).toBe('0px')
    await resizeTo(960, 600)
    expect(frame.style.transform).toBe('scale(1)')
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('covers thumbnails edge to edge at different sizes without restarting the iframe', async () => {
    let resize: ResizeObserverCallback | undefined
    vi.stubGlobal('IntersectionObserver', undefined)
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { resize = callback }
      observe() {}
      disconnect() {}
    })
    const run = { id: 2, status: 'succeeded', html: '<svg viewBox="0 0 960 600"><circle cx="480" cy="300" r="20"/></svg>' } as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { attachTo: document.body, props: { run }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    const originalDocument = frame.srcdoc
    const resizeTo = async (width: number, height: number) => {
      resize?.([{ target: wrapper.element, contentRect: { width, height } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
      await flushPromises()
    }
    for (const [width, height] of [[174, 144], [174, 164], [174, 188], [320, 144]]) {
      await resizeTo(width, height)
      const scale = Number(frame.style.transform.match(/scale\((.+)\)/)?.[1])
      expect(parseFloat(frame.style.width)).toBe(960)
      expect(scale).toBeCloseTo(Math.max(width / 960, height / 600))
      expect(parseFloat(frame.style.height)).toBe(600)
      expect(960 * scale).toBeGreaterThanOrEqual(width)
      expect(600 * scale).toBeGreaterThanOrEqual(height)
      expect(parseFloat(frame.style.top)).toBeCloseTo((height - 600 * scale) / 2)
      expect(parseFloat(frame.style.left)).toBeCloseTo((width - 960 * scale) / 2)
      expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-viewport', width: Math.min(960, width / scale), height: Math.min(600, height / scale) }, '*')
      expect(wrapper.get('iframe').element).toBe(frame)
      expect(frame.srcdoc).toBe(originalDocument)
    }
    await wrapper.setProps({ run: { ...run, duration_ms: 4500 } })
    expect(wrapper.get('iframe').element).toBe(frame)
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts')
    postMessage.mockClear()
    await resizeTo(0, 0)
    expect(frame.style.height).toBe('600px')
    expect(frame.style.transform).toBe('scale(0)')
    await resizeTo(0, 144)
    expect(frame.style.transform).toBe('scale(0)')
    expect(postMessage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('blocks remote loaders and handlers while preserving classic and module inline scripts', () => {
    const result = intelligencePreviewContent('<script src="https://evil.test/x.js" nonce="unsafe"></script><script type="module">document.body.dataset.ready = "module"</script><script>requestAnimationFrame(() => {})</script><script type="importmap">{"imports":{"evil":"https://evil.test"}}</script><svg onload="alert(1)"></svg>')
    const doc = parsePreview(result.document)
    expect(doc.querySelector('script[src],script[type="importmap"],[onload]')).toBeNull()
    expect(doc.querySelector('script[type="module"]')?.textContent).toContain('dataset.ready')
    expect(Array.from(doc.querySelectorAll('script')).some(script => script.textContent === 'requestAnimationFrame(() => {})')).toBe(true)
    expect(doc.querySelector('meta')?.getAttribute('content')).toContain("script-src 'unsafe-inline'; script-src-attr 'none'")
    expect(doc.querySelector('meta')?.getAttribute('content')).not.toContain('nonce-')
  })

  it('satisfies the inherited host nonce policy without authorizing external script sources', () => {
    const hostScript = document.createElement('script')
    hostScript.setAttribute('nonce', 'host-page-nonce')
    document.head.append(hostScript)
    try {
      const result = intelligencePreviewContent('<script>document.body.dataset.ready = "yes"</script>')
      const shell = new DOMParser().parseFromString(result.document, 'text/html')
      const inner = parsePreview(result.document)
      for (const script of [...shell.querySelectorAll('script'), ...inner.querySelectorAll('script')]) expect(script.getAttribute('nonce')).toBe('host-page-nonce')
      expect(shell.querySelector('meta')?.getAttribute('content')).not.toContain('nonce-')
      expect(inner.head.firstElementChild?.getAttribute('http-equiv')).toBe('Content-Security-Policy')
      expect(inner.head.querySelector('script')?.textContent).toContain('intelligence-preview-playback')
    } finally { hostScript.remove() }
  })

  it('keeps an existing iframe when polling replaces the run object and loads a different run', async () => {
    const detail = vi.mocked(intelligenceMonitorAPI.detail)
    detail.mockImplementation(async id => ({ id, status: 'succeeded', html: `<svg><text>Run ${id}</text></svg>` }) as IntelligenceRun)
    const run = { id: 11, status: 'succeeded' } as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { props: { run, large: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const originalFrame = wrapper.get('iframe').element
    expect(detail).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ run: { ...run, duration_ms: 4200 } })
    await flushPromises()
    expect(detail).toHaveBeenCalledTimes(1)
    expect(wrapper.get('iframe').element).toBe(originalFrame)

    await wrapper.setProps({ run: { ...run, id: 12 } })
    await flushPromises()
    expect(detail).toHaveBeenCalledTimes(2)
    expect(detail).toHaveBeenLastCalledWith(12, expect.any(AbortSignal))
    expect(wrapper.get('iframe').element).not.toBe(originalFrame)
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('Run 12')
    wrapper.unmount()
  })

  it('relays only valid parent viewport messages and resends them when the inner artwork is ready', () => {
    const content = intelligencePreviewContent('<svg></svg>', { fit: 'cover' })
    const dom = new JSDOM(content.document, { runScripts: 'outside-only' })
    try {
      const shell = dom.window
      const parent = {}
      Object.defineProperty(shell, 'parent', { value: parent, configurable: true })
      const child = shell.document.querySelector('iframe')!.contentWindow!
      const postMessage = vi.spyOn(child, 'postMessage').mockImplementation(() => undefined)
      shell.eval(shell.document.body.querySelector('script')!.textContent!)
      const send = (data: unknown, source: unknown = parent) => shell.dispatchEvent(new shell.MessageEvent('message', { data, source }))
      const viewport = { type: 'intelligence-preview-viewport', width: 560, height: 600 }
      send(viewport)
      expect(postMessage).toHaveBeenCalledWith(viewport, '*')
      postMessage.mockClear()
      send({ type: 'intelligence-preview-ready' }, child)
      expect(postMessage.mock.calls).toEqual([[viewport, '*'], [{ type: 'intelligence-preview-playback', playing: false }, '*']])
      postMessage.mockClear()
      send(viewport, {})
      for (const width of [NaN, Infinity, -1, 0, 961, '560']) send({ ...viewport, width })
      send({ ...viewport, height: 601 })
      expect(postMessage).not.toHaveBeenCalled()
    } finally { dom.window.close() }
  })
})
