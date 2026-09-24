import type { IntelligenceRate } from '@/api/admin/intelligenceMonitor'
import DOMPurify from 'dompurify'
import { intelligencePreviewRuntime } from './intelligencePreviewRuntime'

// The generated document lives in a second opaque-origin sandbox. Its trusted
// parent shell's frame-src blocks self-navigation out to another URL as well as
// the resource/connect restrictions inside the artwork. Neither frame receives
// same-origin, navigation, popup, form, download, or storage privileges.
// Keep URL/nonce sources OUT of this policy: inline scripts may read their nonce,
// but must not use it to authorize a remote script. The host's inherited nonce
// policy is satisfied separately on each inline script below.
export const INTELLIGENCE_PREVIEW_CSP = "default-src 'none'; script-src 'unsafe-inline'; script-src-attr 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
const LOCAL_SVG_REFERENCES = new Set(['use', 'textpath', 'mpath', 'pattern', 'lineargradient', 'radialgradient', 'filter', 'animate', 'animatemotion', 'animatetransform', 'set'])
const ANIMATION_TAGS = new Set(['animate', 'animatemotion', 'animatetransform', 'set'])
const ACTIVE_ATTRIBUTES = /^(?:on|href$|xlink:href$|src$|srcset$|action$|formaction$|target$)/i

export function intelligencePreviewContent(html: string, options: { autoplay?: boolean; fit?: 'contain' | 'cover' } = {}): { document: string; scriptsDisabled: boolean } {
  const nonce = document.querySelector<HTMLScriptElement>('script[nonce]')?.nonce || ''
  // Template content is inert even during parsing. Never insert remote markup
  // into the administrator's live document or let a parser load remote assets.
  const template = document.createElement('template')
  const root = template.content.ownerDocument.createElement('html')
  root.innerHTML = html
  template.content.append(root)
  const elements = Array.from(template.content.querySelectorAll('*'))
  const scriptsDisabled = elements.some(node => (node.localName.toLowerCase() === 'script' && node.hasAttribute('src')) || Array.from(node.attributes).some(attr => /^on/i.test(attr.name)))
  // Strip navigation elements themselves: SVG SMIL can restore an anchor href
  // after sanitization, so merely removing its initial href is insufficient.
  template.content.querySelectorAll('a, form').forEach(node => node.replaceWith(...Array.from(node.childNodes)))
  template.content.querySelectorAll('base, meta, link, iframe, frame, frameset, object, embed, area, template').forEach(node => node.remove())
  template.content.querySelectorAll('script').forEach(node => {
    const type = (node.getAttribute('type') || '').trim().toLowerCase()
    if (node.hasAttribute('src') || !['', 'text/javascript', 'application/javascript', 'module'].includes(type)) {
      node.remove()
      return
    }
    // Recreate inline scripts after sanitizing, without remote loaders, nonces,
    // async flags, legacy for/event handlers or arbitrary generated attributes.
    for (const attr of Array.from(node.attributes)) if (attr.name !== 'type') node.removeAttribute(attr.name)
    if (type) node.setAttribute('type', type)
  })
  template.content.querySelectorAll('*').forEach(node => {
    const tag = node.localName.toLowerCase()
    if (ANIMATION_TAGS.has(tag) && ACTIVE_ATTRIBUTES.test(node.getAttribute('attributeName') || '')) {
      node.remove()
      return
    }
    for (const attr of Array.from(node.attributes)) {
      const name = attr.name.toLowerCase()
      if (/^on/i.test(name) || ['action', 'formaction', 'target', 'ping', 'srcset', 'srcdoc'].includes(name)) node.removeAttribute(attr.name)
      else if (name === 'href' || name === 'xlink:href') {
        if (!LOCAL_SVG_REFERENCES.has(tag) || !/^#[^\s]+$/.test(attr.value)) node.removeAttribute(attr.name)
      } else if (name === 'src' && !(tag === 'img' && /^data:image\//i.test(attr.value))) node.removeAttribute(attr.name)
    }
  })
  const content = DOMPurify.sanitize(root, {
    USE_PROFILES: { html: true, svg: true, svgFilters: true },
    ADD_TAGS: ['animate', 'animateMotion', 'animateTransform', 'set', 'mpath', 'use', 'script'],
    ADD_ATTR: ['from', 'to', 'calcMode'],
    FORBID_TAGS: ['a', 'area', 'base', 'meta', 'link', 'iframe', 'frame', 'frameset', 'object', 'embed', 'form', 'template'],
    FORBID_ATTR: ['action', 'formaction', 'target', 'ping', 'srcdoc', 'srcset'],
    WHOLE_DOCUMENT: true,
    RETURN_DOM: true,
  })
  const doc = document.implementation.createHTMLDocument('')
  const safeRoot = content as HTMLElement
  const csp = doc.createElement('meta')
  csp.setAttribute('http-equiv', 'Content-Security-Policy')
  csp.setAttribute('content', INTELLIGENCE_PREVIEW_CSP)
  doc.head.replaceChildren(csp)
  const runtime = doc.createElement('script')
  if (nonce) runtime.setAttribute('nonce', nonce)
  runtime.textContent = intelligencePreviewRuntime(options.autoplay ?? false, options.fit ?? 'contain')
  doc.head.append(runtime)
  const style = doc.createElement('style')
  style.textContent = 'html{color-scheme:light}body{margin:0}*{box-sizing:border-box}'
  doc.head.append(style)
  const safeHead = safeRoot.querySelector('head')
  if (safeHead) doc.head.append(...Array.from(safeHead.childNodes))
  const safeBody = safeRoot.querySelector('body')
  if (safeBody) {
    for (const attr of Array.from(safeBody.attributes)) doc.body.setAttribute(attr.name, attr.value)
    doc.body.append(...Array.from(safeBody.childNodes))
  }
  for (const node of Array.from(doc.querySelectorAll('script'))) {
    if (node === runtime) continue
    const script = doc.createElement('script')
    if (nonce) script.setAttribute('nonce', nonce)
    if (node.getAttribute('type') === 'module') script.type = 'module'
    script.textContent = node.textContent
    node.replaceWith(script)
  }
  // The outer shell contains only our own markup and relay. Never place remote
  // content directly in it, otherwise the child could remove its navigation guard.
  const shell = document.implementation.createHTMLDocument('')
  shell.head.replaceChildren(csp.cloneNode(true))
  const shellStyle = shell.createElement('style')
  shellStyle.textContent = 'html,body{margin:0;width:100%;height:100%;overflow:hidden;background:#fff}iframe{display:block;width:100%;height:100%;border:0}'
  shell.head.append(shellStyle)
  const frame = shell.createElement('iframe')
  frame.setAttribute('sandbox', 'allow-scripts')
  frame.setAttribute('credentialless', '')
  frame.setAttribute('referrerpolicy', 'no-referrer')
  frame.setAttribute('scrolling', 'no')
  frame.title = 'Artwork'
  frame.srcdoc = '<!doctype html>\n' + doc.documentElement.outerHTML
  shell.body.append(frame)
  const relay = shell.createElement('script')
  if (nonce) relay.setAttribute('nonce', nonce)
  relay.textContent = `(() => {
    const frame = document.querySelector('iframe');
    let playing = ${options.autoplay ? 'true' : 'false'};
    let viewport = null;
    const sync = () => {
      if (viewport) frame.contentWindow?.postMessage(viewport, '*');
      frame.contentWindow?.postMessage({type:'intelligence-preview-playback',playing}, '*');
    };
    window.addEventListener('message', event => {
      if (event.source === parent && event.data?.type === 'intelligence-preview-playback' && typeof event.data.playing === 'boolean') {
        playing = event.data.playing; sync();
      } else if (event.source === parent && event.data?.type === 'intelligence-preview-viewport') {
        const {width, height} = event.data;
        if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0 || width > 960 || height > 600) return;
        viewport = {type:'intelligence-preview-viewport',width,height}; sync();
      } else if (event.source === frame.contentWindow && event.data?.type === 'intelligence-preview-ready') sync();
    });
    frame.addEventListener('load', sync);
  })();`
  shell.body.append(relay)
  return { document: '<!doctype html>\n' + shell.documentElement.outerHTML, scriptsDisabled }
}

export function intelligencePreviewDocument(html: string): string {
  return intelligencePreviewContent(html).document
}
export function intelligenceRateValue(rate: IntelligenceRate): number | null {
  if (!rate) return null
  const candidate = rate.effective_rate_multiplier
  return typeof candidate === 'number' && Number.isFinite(candidate) && candidate >= 0 ? candidate : null
}
export function intelligenceRateLabel(rate: IntelligenceRate): string | null {
  const value = intelligenceRateValue(rate)
  return value === null ? null : `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(value)}×`
}
export function intelligenceNotes(value: unknown): string {
  if (typeof value === 'string') return value
  if (!value || typeof value !== 'object') return ''
  return ['supplier_note', 'group_note', 'rate_note', 'notes'].map(key => (value as Record<string, unknown>)[key]).filter(part => typeof part === 'string' && part.trim()).join(' · ')
}
