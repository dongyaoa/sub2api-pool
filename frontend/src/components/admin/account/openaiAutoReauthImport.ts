export interface ReauthLineValidation {
  line: number
  email?: string
  error?: 'invalid_format' | 'invalid_email' | 'password_required' | 'invalid_totp_secret' | 'duplicate_email'
}

/** Return only safe metadata. Do not retain passwords or TOTP seeds in preview rows. */
export function validateReauthImport(content: string): ReauthLineValidation[] {
  const seen = new Set<string>()
  return content.split(/\r?\n/).flatMap((line, index) => {
    if (!line.trim()) return []
    const result: ReauthLineValidation = { line: index + 1 }
    const fields = line.split('----')
    if (fields.length !== 3) return [{ ...result, error: 'invalid_format' as const }]
    const email = fields[0].trim()
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      return [{ ...result, error: 'invalid_email' as const }]
    }
    result.email = email
    if (fields[1].length === 0) result.error = 'password_required'
    else if (!/^[A-Z2-7]{16,}={0,6}$/i.test(fields[2].replace(/\s/g, ''))) result.error = 'invalid_totp_secret'
    else if (seen.has(email.toLowerCase())) result.error = 'duplicate_email'
    seen.add(email.toLowerCase())
    return [result]
  })
}
