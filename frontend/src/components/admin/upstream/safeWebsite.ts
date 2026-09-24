/** Derive a public-facing website URL without endpoint paths or credentials. */
export function safeWebsite(value: string | null | undefined): string | null {
  if (!value || !/^https?:\/\//i.test(value.trim())) return null

  try {
    const url = new URL(value)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return null
    return url.origin
  } catch {
    return null
  }
}
