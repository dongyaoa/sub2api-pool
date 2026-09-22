import { createHmac } from 'node:crypto';
import { WorkerError } from './errors.mjs';

// RFC 4648 Base32; keep the decoded secret in a Buffer so it can be overwritten.
export function decodeSecret(value) {
  if (typeof value !== 'string') throw new WorkerError('invalid_request', 400);
  let secret = value.trim();
  if (secret.startsWith('otpauth://')) {
    let url;
    try { url = new URL(secret); } catch { throw new WorkerError('invalid_request', 400); }
    if (url.hostname !== 'totp' || (url.searchParams.get('algorithm') || 'SHA1').toUpperCase() !== 'SHA1'
      || (url.searchParams.get('digits') || '6') !== '6' || (url.searchParams.get('period') || '30') !== '30') {
      throw new WorkerError('unsupported_totp', 400);
    }
    secret = url.searchParams.get('secret') || '';
  }
  secret = secret.replace(/\s/g, '').replace(/=+$/, '').toUpperCase();
  if (!/^[A-Z2-7]{16,128}$/.test(secret)) throw new WorkerError('invalid_request', 400);
  let bits = 0;
  let valueBits = 0;
  const bytes = [];
  for (const character of secret) {
    valueBits = (valueBits << 5) | 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(character);
    bits += 5;
    if (bits >= 8) { bits -= 8; bytes.push((valueBits >>> bits) & 255); }
  }
  return Buffer.from(bytes);
}

export function totp(secret, timeMs = Date.now(), digits = 6) {
  const key = decodeSecret(secret);
  try {
    const counter = Buffer.alloc(8);
    counter.writeBigUInt64BE(BigInt(Math.floor(timeMs / 30_000)));
    const digest = createHmac('sha1', key).update(counter).digest();
    const offset = digest[digest.length - 1] & 15;
    const binary = digest.readUInt32BE(offset) & 0x7fffffff;
    return String(binary % 10 ** digits).padStart(digits, '0');
  } finally { key.fill(0); }
}
