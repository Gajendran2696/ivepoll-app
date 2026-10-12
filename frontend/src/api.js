// All network access lives here, so pages never build URLs or headers themselves.

const API = (import.meta.env.VITE_API_URL || '').replace(/\/$/, '')
const TOKEN_KEY = 'livepoll_token'
const VOTER_KEY = 'livepoll_voter'

export const tokenStore = {
  get: () => safe(() => localStorage.getItem(TOKEN_KEY)),
  set: (t) => safe(() => localStorage.setItem(TOKEN_KEY, t)),
  clear: () => safe(() => localStorage.removeItem(TOKEN_KEY)),
}

function safe(fn) {
  try {
    return fn()
  } catch {
    return null // storage can be blocked (private mode); the app still works for the session
  }
}

// Anonymous voters get a random ID kept in this browser. The server hashes it and enforces
// one vote per poll with a unique database index.
export function voterId() {
  let id = safe(() => localStorage.getItem(VOTER_KEY))
  if (!id) {
    id = crypto.randomUUID()
    safe(() => localStorage.setItem(VOTER_KEY, id))
  }
  return id
}

export class ApiError extends Error {
  constructor(message, status) {
    super(message)
    this.status = status
  }
}

async function request(path, { method = 'GET', body, auth = false, voter = false } = {}) {
  const headers = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const token = tokenStore.get()
  if (token && (auth || path.startsWith('/api/polls'))) headers.Authorization = `Bearer ${token}`
  if (voter) headers['X-Voter-Id'] = voterId()

  let res
  try {
    res = await fetch(API + path, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError('Cannot reach the server. Check your connection and try again.', 0)
  }

  if (res.status === 204) return null
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401 && auth) window.dispatchEvent(new Event('livepoll:unauthorized'))
    throw new ApiError(data.error || `Request failed (${res.status})`, res.status)
  }
  return data
}

export const api = {
  signup: (b) => request('/api/auth/signup', { method: 'POST', body: b }),
  login: (b) => request('/api/auth/login', { method: 'POST', body: b }),
  me: () => request('/api/auth/me', { auth: true }),
  createPoll: (b) => request('/api/polls', { method: 'POST', body: b, auth: true }),
  myPolls: () => request('/api/polls', { auth: true }),
  getPoll: (slug) => request(`/api/polls/${slug}`, { voter: true }),
  vote: (slug, optionId) =>
    request(`/api/polls/${slug}/vote`, { method: 'POST', body: { optionId }, voter: true }),
  setClosed: (slug, closed) =>
    request(`/api/polls/${slug}`, { method: 'PATCH', body: { closed }, auth: true }),
  deletePoll: (slug) => request(`/api/polls/${slug}`, { method: 'DELETE', auth: true }),
}

export function wsUrl(path) {
  if (API) return API.replace(/^http/, 'ws') + path
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}
