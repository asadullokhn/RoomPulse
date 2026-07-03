package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HistoryEvent is one timestamped line from a device's in-app event log.
type HistoryEvent struct {
	TS     int64  `json:"ts"` // epoch millis on the device
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// HistoryReport is a full event-log dump a phone sends on demand, so we can see
// exactly what fired (region events, check-in/out, notifications, lifecycle)
// while it was backgrounded/locked. Kept in a small in-memory ring.
type HistoryReport struct {
	DeviceID   string         `json:"device_id"`
	Name       string         `json:"name"`
	Events     []HistoryEvent `json:"events"`
	ReceivedAt time.Time      `json:"received_at"`
}

type historyBuffer struct {
	mu   sync.Mutex
	max  int
	list []HistoryReport
}

func newHistoryBuffer(max int) *historyBuffer { return &historyBuffer{max: max} }

func (b *historyBuffer) add(r HistoryReport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.list = append(b.list, r)
	if len(b.list) > b.max {
		b.list = b.list[len(b.list)-b.max:]
	}
}

func (b *historyBuffer) recent() []HistoryReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]HistoryReport, len(b.list))
	for i, r := range b.list {
		out[len(b.list)-1-i] = r // newest first
	}
	return out
}

func (s *Server) postHistory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID string         `json:"device_id"`
		Name     string         `json:"name"`
		Events   []HistoryEvent `json:"events"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.DeviceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "device_id required")
		return
	}
	body.DeviceID = clamp(body.DeviceID, maxIDLen)
	body.Name = clamp(body.Name, maxNameLen)
	if len(body.Events) > 2000 { // keep the newest 2000
		body.Events = body.Events[len(body.Events)-2000:]
	}
	for i := range body.Events {
		body.Events[i].Kind = clamp(body.Events[i].Kind, 48)
		body.Events[i].Detail = clamp(body.Events[i].Detail, 240)
	}
	s.history.add(HistoryReport{
		DeviceID:   body.DeviceID,
		Name:       body.Name,
		Events:     body.Events,
		ReceivedAt: time.Now(),
	})
	s.log.Info("device history", "device", body.DeviceID, "name", body.Name, "events", len(body.Events))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "events": len(body.Events)})
}

// getHistory returns recent dumps. ?format=text renders the newest as a plain
// timeline (easiest to eyeball over curl); default is JSON.
func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	reports := s.history.recent()
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		var b strings.Builder
		for _, rep := range reports {
			fmt.Fprintf(&b, "=== %s (%s) · %d events · received %s ===\n",
				rep.Name, rep.DeviceID, len(rep.Events), rep.ReceivedAt.UTC().Format(time.RFC3339))
			for _, e := range rep.Events {
				t := time.UnixMilli(e.TS).UTC().Format("15:04:05.000")
				fmt.Fprintf(&b, "  %s  %-18s %s\n", t, e.Kind, e.Detail)
			}
			b.WriteString("\n")
		}
		_, _ = w.Write([]byte(b.String()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}
