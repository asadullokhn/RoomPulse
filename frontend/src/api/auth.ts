const TOKEN_KEY = 'qr_admin_jwt'
const EMAIL_KEY = 'qr_admin_email'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(EMAIL_KEY)
}

export function getAdminEmail(): string {
  return localStorage.getItem(EMAIL_KEY) ?? ''
}

export async function login(email: string, password: string): Promise<void> {
  const res = await fetch('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error ?? 'login failed')
  }
  const data = (await res.json()) as { token: string; email: string }
  setToken(data.token)
  localStorage.setItem(EMAIL_KEY, data.email)
}

export function logout() {
  clearToken()
  window.location.assign('/login')
}
