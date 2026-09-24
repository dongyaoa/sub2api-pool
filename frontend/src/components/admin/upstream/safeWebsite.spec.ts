import { describe, expect, it } from 'vitest'
import { safeWebsite } from './safeWebsite'

describe('safeWebsite', () => {
  it.each([
    ['https://user:password@example.com/private/sk-secret/v1?api_key=secret#secret', 'https://example.com'],
    ['http://example.com:8080/v1', 'http://example.com:8080'],
    ['https://example.com:443/path', 'https://example.com'],
    [' HTTPS://EXAMPLE.COM/v1 ', 'https://example.com'],
    ['https://[2001:db8::1]:8443/v1?key=secret', 'https://[2001:db8::1]:8443'],
  ])('returns only the sanitized origin of %s', (input, expected) => {
    expect(safeWebsite(input)).toBe(expected)
  })

  it.each([
    undefined, null, '', ' ', 'not a URL', '/v1', '//example.com/v1',
    'https:example.com', 'https://', 'https://example .com',
    'javascript:alert(1)', 'data:text/html,secret', 'file:///secret',
    'ftp://example.com/v1', 'blob:https://example.com/secret',
  ])('rejects a missing, invalid or non-HTTP website: %s', input => {
    expect(safeWebsite(input)).toBeNull()
  })
})
