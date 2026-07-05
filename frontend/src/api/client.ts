import type {
  Room, Reservation, OccupancyEntry, Device, Beacon, EventEntry,
  Utilization, Collision, Overstay, Notification, FloorData, Info, User,
} from './types'
import { getToken, clearToken } from './auth'

// authFetch attaches the admin JWT and funnels every 401 back to the login
// screen (expired token, deleted admin, restarted backend with a new secret).
async function authFetch(url: string, init?: RequestInit): Promise<Response> {
  const token = getToken()
  const headers = new Headers(init?.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(url, { ...init, headers })
  if (res.status === 401) {
    clearToken()
    window.location.assign('/#/login')
    throw new Error('session expired')
  }
  return res
}

async function getJSON<T>(url: string): Promise<T> {
  const res = await authFetch(url)
  if (!res.ok) throw new Error(`${url}: ${res.status}`)
  return res.json() as Promise<T>
}

async function sendJSON<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await authFetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const payload = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(payload.error ?? res.statusText)
  }
  return res.json() as Promise<T>
}

export const getRooms = () => getJSON<{ rooms: Room[] }>('/rooms').then(d => d.rooms ?? [])
export const getReservations = () => getJSON<{ reservations: Reservation[] }>('/reservations').then(d => d.reservations ?? [])
export const getOccupancy = () => getJSON<{ occupancy: OccupancyEntry[] }>('/occupancy').then(d => d.occupancy ?? [])
export const getDevices = () => getJSON<{ devices: Device[] }>('/devices').then(d => d.devices ?? [])
export const getBeacons = () => getJSON<{ beacons: Beacon[] }>('/beacons').then(d => d.beacons ?? [])
export const putBeacon = (workspaceId: string, body: { uuid: string; major: number; minor: number }) =>
  sendJSON<Beacon>('PUT', `/beacons/${encodeURIComponent(workspaceId)}`, body)
export const deleteBeacon = (workspaceId: string) =>
  sendJSON<{ status: string }>('DELETE', `/beacons/${encodeURIComponent(workspaceId)}`).then(() => undefined)
export const getEvents = (workspaceId: string, limit = 25) =>
  getJSON<{ events: EventEntry[] }>(`/events?workspace_id=${encodeURIComponent(workspaceId)}&limit=${limit}`).then(d => d.events ?? [])
export const getUtilization = () => getJSON<Utilization>('/utilization')
export const getCollisions = () => getJSON<{ collisions: Collision[] }>('/collisions').then(d => d.collisions ?? [])
export const getOverstays = () => getJSON<{ overstays: Overstay[] }>('/overstays').then(d => d.overstays ?? [])
export const getNotifications = (limit = 30) => getJSON<{ notifications: Notification[] }>(`/notifications?limit=${limit}`).then(d => d.notifications ?? [])
export const getFloorRooms = () => getJSON<FloorData>('/floor/rooms')
export const getInfo = () => getJSON<Info>('/info')
export const postSync = () => authFetch('/sync', { method: 'POST' })
export const getUsers = () => getJSON<{ users: User[] }>('/users').then(d => d.users ?? [])
export const getUserReservations = (userId: string) =>
  getJSON<{ reservations: Reservation[] }>(`/users/${encodeURIComponent(userId)}/reservations`).then(d => d.reservations ?? [])
export const deleteUser = (userId: string) =>
  sendJSON<{ status: string }>('DELETE', `/users/${encodeURIComponent(userId)}`).then(() => undefined)
export const renameUser = (userId: string, name: string) =>
  sendJSON<{ status: string }>('PATCH', `/users/${encodeURIComponent(userId)}`, { name }).then(() => undefined)
export const adminCancelReservation = (reservationId: string) =>
  sendJSON<Reservation>('POST', `/admin/reservations/${encodeURIComponent(reservationId)}/cancel`)
export const adminCreateReservation = (body: { workspace_id: string; start_time: string; end_time: string; user_email?: string }) =>
  sendJSON<Reservation>('POST', '/admin/reservations', body)
export const adminPatchReservation = (reservationId: string, body: { start_time?: string; end_time?: string }) =>
  sendJSON<Reservation>('PATCH', `/admin/reservations/${encodeURIComponent(reservationId)}`, body)
export const createRoom = (body: { name: string; capacity: number; has_tv: boolean }) =>
  sendJSON<Room>('POST', '/rooms', body)
export const patchRoom = (workspaceId: string, body: { name?: string; capacity?: number; has_tv?: boolean }) =>
  sendJSON<Room>('PATCH', `/rooms/${encodeURIComponent(workspaceId)}`, body)
export const deleteRoom = (workspaceId: string) =>
  sendJSON<{ status: string }>('DELETE', `/rooms/${encodeURIComponent(workspaceId)}`)
export const deleteNotification = (id: number) =>
  sendJSON<{ status: string }>('DELETE', `/notifications/${id}`).then(() => undefined)
export const clearNotifications = () =>
  sendJSON<{ status: string }>('DELETE', '/notifications').then(() => undefined)
