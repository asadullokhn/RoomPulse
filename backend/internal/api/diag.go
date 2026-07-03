package api

import (
	"net/http"
	"sync"
	"time"
)

// DiagRow mirrors one checklist line from the app's diagnostics panel.
type DiagRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Level string `json:"level"`
	Hint  string `json:"hint,omitempty"`
}

// DiagReport is a point-in-time health snapshot POSTed by a phone. Kept in a
// small in-memory ring so a device's check-in readiness can be eyeballed
// remotely via GET /diag — handy when the phone isn't in front of you.
type DiagReport struct {
	DeviceID    string    `json:"device_id"`
	DisplayName string    `json:"display_name"`
	Summary     string    `json:"summary"`
	Ready       bool      `json:"ready"`
	Rows        []DiagRow `json:"rows"`
	Line        string    `json:"line"`
	ReceivedAt  time.Time `json:"received_at"`
}

// diagBuffer is a fixed-size, newest-last ring of recent reports. Diagnostics
// are ephemeral debugging aids, so they live in memory only (no DB).
type diagBuffer struct {
	mu   sync.Mutex
	max  int
	list []DiagReport
}

func newDiagBuffer(max int) *diagBuffer { return &diagBuffer{max: max} }

func (b *diagBuffer) add(r DiagReport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.list = append(b.list, r)
	if len(b.list) > b.max {
		b.list = b.list[len(b.list)-b.max:]
	}
}

// recent returns a newest-first copy of the buffer.
func (b *diagBuffer) recent() []DiagReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]DiagReport, len(b.list))
	for i, r := range b.list {
		out[len(b.list)-1-i] = r
	}
	return out
}

// postDiag accepts a device's diagnostics snapshot.
func (s *Server) postDiag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID    string    `json:"device_id"`
		DisplayName string    `json:"display_name"`
		Summary     string    `json:"summary"`
		Ready       bool      `json:"ready"`
		Rows        []DiagRow `json:"rows"`
		Line        string    `json:"line"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.DeviceID) == 0 || len(body.DeviceID) > maxIDLen {
		writeError(w, http.StatusUnprocessableEntity, "device_id required")
		return
	}
	body.DisplayName = clamp(body.DisplayName, maxNameLen)
	body.Summary = clamp(body.Summary, 200)
	body.Line = clamp(body.Line, 500)
	if len(body.Rows) > 20 {
		body.Rows = body.Rows[:20]
	}
	for i := range body.Rows {
		body.Rows[i].Label = clamp(body.Rows[i].Label, 64)
		body.Rows[i].Value = clamp(body.Rows[i].Value, 160)
		body.Rows[i].Level = clamp(body.Rows[i].Level, 16)
		body.Rows[i].Hint = clamp(body.Rows[i].Hint, 200)
	}
	s.diags.add(DiagReport{
		DeviceID:    body.DeviceID,
		DisplayName: body.DisplayName,
		Summary:     body.Summary,
		Ready:       body.Ready,
		Rows:        body.Rows,
		Line:        body.Line,
		ReceivedAt:  time.Now(),
	})
	s.log.Info("device diagnostics", "device", body.DeviceID, "name", body.DisplayName, "ready", body.Ready, "line", body.Line)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// getDiag returns recent reports, newest first.
func (s *Server) getDiag(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"reports": s.diags.recent()})
}
