import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { intelligenceMonitorAPI, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import { intelligencePreviewContent, intelligencePreviewDocument } from './intelligencePreview'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail: vi.fn() } }))
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

function parsePreview(value: string): Document {
  return new DOMParser().parseFromString(value, 'text/html')
}

describe('intelligence preview isolation', () => {
  it('removes active HTML, SVG navigation and remote resource surfaces', () => {
    const result = intelligencePreviewContent(`<!doctype html><html><head><meta http-equiv="refresh" content="0;url=https://evil.test"><link rel="stylesheet" href="https://evil.test/x.css"></head><body onload="fetch('/session')"><a href="https://evil.test" target="_top">go</a><form action="https://evil.test"><input formaction="https://evil.test"></form><iframe src="https://evil.test"></iframe><svg><use href="https://evil.test/icon.svg#x"/><image href="https://evil.test/a.png"/><animate attributeName="href" to="https://evil.test"/></svg><script>location='https://evil.test'</script></body></html>`)
    const doc = parsePreview(result.document)

    expect(result.scriptsDisabled).toBe(true)
    expect(doc.querySelector('script,meta[http-equiv="refresh"],link,iframe,form,a,base')).toBeNull()
    expect(doc.querySelector('[onload], [formaction], [target], [href^="http"], [src^="http"]')).toBeNull()
    expect(doc.querySelector('svg use')).not.toBeNull()
    expect(doc.querySelector('svg use')?.getAttribute('href')).toBeNull()
    expect(doc.querySelector('svg animate')).toBeNull()
    expect(doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')).toContain("script-src 'none'")
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

  it('keeps all sandbox privileges disabled and preserves the raw run for source/download', async () => {
    const raw = '<svg><script>window.top.location = "https://evil.test"</script><circle /></svg>'
    const run = Object.freeze({ id: 1, status: 'succeeded', html: raw }) as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { props: { run, large: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes('credentialless')).toBeDefined()
    expect(frame.attributes('srcdoc')).not.toContain('<script>')
    expect(frame.attributes('srcdoc')).toContain("script-src 'none'")
    expect(wrapper.get('[role="note"]').text()).toBe('intelligenceMonitor.scriptsDisabled')
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

  it('fills taller thumbnails with a proportional HTML viewport without cropping or restarting the iframe', async () => {
    let resize: ResizeObserverCallback | undefined
    vi.stubGlobal('IntersectionObserver', undefined)
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { resize = callback }
      observe() {}
      disconnect() {}
    })
    const run = { id: 2, status: 'succeeded', html: '<svg viewBox="0 0 960 600"><circle cx="480" cy="300" r="20"/></svg>' } as IntelligenceRun
    const wrapper = mount(IntelligenceArtifactPreview, { props: { run }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    const resizeTo = async (width: number, height: number) => {
      resize?.([{ target: wrapper.element, contentRect: { width, height } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
      await flushPromises()
    }
    for (const height of [144, 164, 188]) {
      await resizeTo(174, height)
      const scale = Number(frame.style.transform.match(/scale\((.+)\)/)?.[1])
      expect(parseFloat(frame.style.width)).toBe(960)
      expect(scale).toBeCloseTo(174 / 960)
      expect(parseFloat(frame.style.height) * scale).toBeCloseTo(height)
      expect(parseFloat(frame.style.top)).toBeCloseTo(0)
      expect(parseFloat(frame.style.left)).toBeCloseTo(0)
      expect(wrapper.get('iframe').element).toBe(frame)
    }
    await wrapper.setProps({ run: { ...run, duration_ms: 4500 } })
    expect(wrapper.get('iframe').element).toBe(frame)
    expect(frame.getAttribute('sandbox')).toBe('')
    await resizeTo(0, 0)
    expect(frame.style.height).toBe('600px')
    expect(frame.style.transform).toBe('scale(0)')
    wrapper.unmount()
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
})
