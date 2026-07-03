import type {
  Room, Reservation, OccupancyEntry, Device, Beacon, EventEntry,
  Utilization, Collision, Overstay, Notification, FloorData, Info,
} from './types'

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${url}: ${res.status}`)
  return res.json() as Promise<T>
}

export const getRooms = () => getJSON<{ rooms: Room[] }>('/rooms').then(d => d.rooms ?? [])
export const getReservations = () => getJSON<{ reservations: Reservation[] }>('/reservations').then(d => d.reservations ?? [])
export const getOccupancy = () => getJSON<{ occupancy: OccupancyEntry[] }>('/occupancy').then(d => d.occupancy ?? [])
export const getDevices = () => getJSON<{ devices: Device[] }>('/devices').then(d => d.devices ?? [])
export const getBeacons = () => getJSON<{ beacons: Beacon[] }>('/beacons').then(d => d.beacons ?? [])
export const putBeacon = (workspaceId: string, body: { uuid: string; major: number; minor: number }) =>
  fetch(`/beacons/${encodeURIComponent(workspaceId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }).then(async r => {
    if (!r.ok) throw new Error((await r.json().catch(() => ({ error: r.statusText }))).error ?? r.statusText)
    return r.json() as Promise<Beacon>
  })
export const deleteBeacon = (workspaceId: string) =>
  fetch(`/beacons/${encodeURIComponent(workspaceId)}`, { method: 'DELETE' }).then(r => {
    if (!r.ok) throw new Error(r.statusText)
  })
export const getEvents = (workspaceId: string, limit = 25) =>
  getJSON<{ events: EventEntry[] }>(`/events?workspace_id=${encodeURIComponent(workspaceId)}&limit=${limit}`).then(d => d.events ?? [])
export const getUtilization = () => getJSON<Utilization>('/utilization')
export const getCollisions = () => getJSON<{ collisions: Collision[] }>('/collisions').then(d => d.collisions ?? [])
export const getOverstays = () => getJSON<{ overstays: Overstay[] }>('/overstays').then(d => d.overstays ?? [])
export const getNotifications = (limit = 30) => getJSON<{ notifications: Notification[] }>(`/notifications?limit=${limit}`).then(d => d.notifications ?? [])
export const getFloorRooms = () => getJSON<FloorData>('/floor/rooms')
export const getInfo = () => getJSON<Info>('/info')
export const postSync = () => fetch('/sync', { method: 'POST' })
