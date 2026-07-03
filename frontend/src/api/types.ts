export type CheckInStatus = 'not_checked_in' | 'checked_in' | 'checked_out'
export type ReservationStatus = 'booked' | 'no_show' | 'released' | 'cancelled'

export interface Room {
  room_id: string
  zoom_workspace_id: string
  name: string
  floor: string
  capacity: number
  has_tv: boolean
  is_zoom_room: boolean
  beacon_uuid?: string
  beacon_major?: number
  beacon_minor?: number
}

export interface Reservation {
  reservation_id: string
  room_id: string
  zoom_workspace_id: string
  user_id: string
  user_email?: string
  start_time: string // RFC3339
  end_time: string
  status: ReservationStatus
  check_in_status: CheckInStatus
  source?: 'zoom' | 'app'
  booked_by_user_id?: string
}

export interface User {
  user_id: string
  email?: string
  name?: string
  created_at: string
}

export interface OccupancyEntry {
  workspace_id: string
  count: number
  users: string[]
}

export interface Device {
  device_id: string
  display_name: string
  workspace_id: string // '' = not in any room
  last_seen_sec: number
}

export interface Beacon {
  workspace_id: string
  uuid: string
  major: number
  minor: number
  name: string
}

export interface EventEntry {
  kind: 'enter' | 'leave'
  name: string
  actor: string
  ago_sec: number
}

export interface Utilization {
  bookings: number
  checked_in: number
  no_show_released: number
  booked: number
  no_show_rate: number
  rooms_total: number
  rooms_occupied: number
  people_present: number
  generated_at: string
}

export interface Collision {
  workspace_id: string
  room_name: string
  reservation_id: string
  booker: string
  occupants: string[]
  since: string
}

export interface Overstay {
  workspace_id: string
  room_name: string
  reservation_id: string
  booker: string
  occupants: string[]
  ended_at: string
  over_by_sec: number
}

export interface Notification {
  id: number
  type: 'grace_reminder' | 'no_show_released' | 'room_freed' | 'collision' | 'overstay'
  level?: number
  workspace_id?: string
  reservation_id?: string
  recipient?: string
  title: string
  body: string
  created_at: string
}

export interface FloorRoom {
  name: string
  points: number[][]
  kind: 'room' | 'workspace'
  capacity: number
  screens: number
  busy: boolean
}

export interface FloorData {
  rooms: FloorRoom[]
  view_box: { x: number; y: number; w: number; h: number }
  image: { w: number; h: number }
}

export interface Info {
  zoom_mode: string
  authorized: boolean
}
