import { REDIRECT_URI } from '../src/validation.mjs';

export function validInput() {
  const params = new URLSearchParams({ client_id: 'app_EMoamEEZ73f0CkXaXp7hrann', response_type: 'code',
    redirect_uri: REDIRECT_URI, state: 'fixture-state-with-enough-entropy', code_challenge: 'a'.repeat(43), code_challenge_method: 'S256' });
  return { email: 'fixture@example.com', password: 'fixture-password', totp_secret: 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ',
    auth_url: `https://auth.openai.com/oauth/authorize?${params}`, redirect_uri: REDIRECT_URI, proxy_url: 'http://127.0.0.1:19090' };
}
