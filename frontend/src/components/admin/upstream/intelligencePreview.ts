import type { IntelligenceRate } from '@/api/admin/intelligenceMonitor'
import DOMPurify from 'dompurify'

// An opaque iframe alone cannot prevent script-driven self-navigation. Keep
// scripts disabled in both CSP and the iframe sandbox. CSS and SVG SMIL animate
// without script privileges; the original HTML is kept separately for download.
export const INTELLIGENCE_PREVIEW_CSP = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
const LOCAL_SVG_REFERENCES = new Set(['use', 'textpath', 'mpath', 'pattern', 'lineargradient', 'radialgradient', 'filter', 'animate', 'animatemotion', 'animatetransform', 'set'])
const ANIMATION_TAGS = new Set(['animate', 'animatemotion', 'animatetransform', 'set'])
const ACTIVE_ATTRIBUTES = /^(?:on|href$|xlink:href$|src$|srcset$|action$|formaction$|target$)/i

export function intelligencePreviewContent(html: string): { document: string; scriptsDisabled: boolean } {
  // Template content is inert even during parsing. Never insert remote markup
  // into the administrator's live document or let a parser load remote assets.
  const template = document.createElement('template')
  const root = template.content.ownerDocument.createElement('html')
  root.innerHTML = html
  template.content.append(root)
  const elements = Array.from(template.content.querySelectorAll('*'))
  const scriptsDisabled = elements.some(node => node.localName.toLowerCase() === 'script' || Array.from(node.attributes).some(attr => /^on/i.test(attr.name) || /^\s*javascript:/i.test(attr.value)))
  // Strip navigation elements themselves: SVG SMIL can restore an anchor href
  // after sanitization, so merely removing its initial href is insufficient.
  template.content.querySelectorAll('a, form').forEach(node => node.replaceWith(...Array.from(node.childNodes)))
  template.content.querySelectorAll('script, base, meta, link, iframe, frame, frameset, object, embed, area, template').forEach(node => node.remove())
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
    ADD_TAGS: ['animate', 'animateMotion', 'animateTransform', 'set', 'mpath', 'use'],
    ADD_ATTR: ['from', 'to', 'calcMode'],
    FORBID_TAGS: ['a', 'area', 'base', 'meta', 'link', 'iframe', 'frame', 'frameset', 'object', 'embed', 'form', 'script', 'template'],
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
  return { document: '<!doctype html>\n' + doc.documentElement.outerHTML, scriptsDisabled }
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
