// Package sync pulls Zoom workspaces + reservations and projects them into the
// local store. This is the prototype's whole point: prove QuickRoom can keep a
// faithful local mirror of Zoom (the source of truth) to drive beacon logic on.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"quickroom/internal/domain"
	"quickroom/internal/store"
	"quickroom/internal/zoom"
)

// Service syncs Zoom -> local store.
type Service struct {
	zoom       zoom.Client
	store      *store.Memory
	db         *store.DB // nil-safe: custom rooms/overrides skipped when absent
	locationID string
	log        *slog.Logger
}

// Result reports what a sync run touched.
type Result struct {
	Rooms        int       `json:"rooms"`
	Reservations int       `json:"reservations"`
	SyncedAt     time.Time `json:"synced_at"`
}

func New(zc zoom.Client, st *store.Memory, db *store.DB, locationID string, log *slog.Logger) *Service {
	return &Service{zoom: zc, store: st, db: db, locationID: locationID, log: log}
}

// Run performs one full sync: workspaces -> rooms, then today's reservations.
func (s *Service) Run(ctx context.Context, now time.Time) (Result, error) {
	rooms, err := s.syncWorkspaces(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("sync workspaces: %w", err)
	}
	res, err := s.syncReservations(ctx, now)
	if err != nil {
		return Result{}, fmt.Errorf("sync reservations: %w", err)
	}
	s.applyAdminRooms()
	r := Result{Rooms: rooms, Reservations: res, SyncedAt: now}
	s.log.Info("sync complete", "rooms", rooms, "reservations", res)
	return r, nil
}

// applyAdminRooms re-asserts admin room state after a Zoom sync: custom rooms
// are re-upserted (they have no Zoom source) and overrides re-patch the
// Zoom-synced mirror so admin edits always win over the pull.
func (s *Service) applyAdminRooms() {
	if s.db == nil {
		return
	}
	custom, err := s.db.CustomRooms()
	if err != nil {
		s.log.Warn("load custom rooms", "err", err)
	}
	for _, r := range custom {
		s.store.UpsertRoom(r)
	}
	overrides, err := s.db.RoomOverrides()
	if err != nil {
		s.log.Warn("load room overrides", "err", err)
	}
	for _, o := range overrides {
		room, ok := s.store.RoomByWorkspace(o.WorkspaceID)
		if !ok {
			continue
		}
		if o.Name != "" {
			room.Name = o.Name
		}
		if o.Capacity >= 0 {
			room.Capacity = o.Capacity
		}
		if o.HasTV >= 0 {
			room.HasTV = o.HasTV == 1
		}
		s.store.UpsertRoom(room)
	}
}

func (s *Service) syncWorkspaces(ctx context.Context) (int, error) {
	ws, err := s.zoom.ListWorkspaces(ctx, s.locationID)
	if err != nil {
		return 0, err
	}
	for _, w := range ws {
		s.store.UpsertRoom(domain.Room{
			RoomID:          "room-" + w.ID, // local id derived from workspace id
			ZoomWorkspaceID: w.ID,
			Name:            w.Name,
			Capacity:        w.Capacity,
			HasTV:           w.HasTV,
			IsZoomRoom:      w.HasTV, // ZoomRoom-typed spaces have equipment
			// Beacon fields intentionally left empty: mapped from local config,
			// and UpsertRoom preserves any prior mapping.
		})
	}
	return len(ws), nil
}

func (s *Service) syncReservations(ctx context.Context, now time.Time) (int, error) {
	// Prototype window: start of today -> +24h.
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	to := from.Add(24 * time.Hour)

	res, err := s.zoom.ListReservations(ctx, s.locationID, from, to)
	if err != nil {
		return 0, err
	}
	for _, r := range res {
		room, ok := s.store.RoomByWorkspace(r.WorkspaceID)
		if !ok {
			// User mode can't list workspaces; synthesize a room from the
			// reservation so /rooms still shows what we can see.
			name := r.WorkspaceName
			if name == "" {
				name = "Workspace " + r.WorkspaceID
			}
			room = domain.Room{
				RoomID:          "room-" + r.WorkspaceID,
				ZoomWorkspaceID: r.WorkspaceID,
				Name:            name,
				Floor:           r.LocationName,
			}
			s.store.UpsertRoom(room)
		}
		roomID := room.RoomID
		s.store.UpsertReservation(domain.Reservation{
			ReservationID:   r.ReservationID,
			RoomID:          roomID,
			ZoomWorkspaceID: r.WorkspaceID,
			UserID:          r.UserID,
			UserEmail:       r.UserEmail,
			StartTime:       r.StartTime,
			EndTime:         r.EndTime,
			Status:          domain.StatusBooked,
			CheckInStatus:   mapCheckIn(r.CheckInStatus),
			Source:          "zoom",
		})
	}
	return len(res), nil
}

func mapCheckIn(zoomStatus string) domain.CheckInStatus {
	switch zoomStatus {
	case "checked_in":
		return domain.CheckedIn
	case "checked_out":
		return domain.CheckedOut
	default:
		return domain.NotCheckedIn
	}
}
