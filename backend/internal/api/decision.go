package api

import (
	"net/http"
	"sync"
	"time"
)

// Decision is a "what should we build next" choice submitted from /decide.
// Kept in memory (latest + a short history) so it can be read back via GET
// /decision — a lightweight way to answer a question from the browser.
type Decision struct {
	Choice     string    `json:"choice"` // option key: scenarios|zoom|demo|config|custom
	Label      string    `json:"label"`  // human-readable label
	Custom     string    `json:"custom"` // free text when choice == "custom"
	Note       string    `json:"note"`   // optional extra context
	ReceivedAt time.Time `json:"received_at"`
}

type decisionStore struct {
	mu   sync.Mutex
	last *Decision
	all  []Decision
}

func newDecisionStore() *decisionStore { return &decisionStore{} }

func (d *decisionStore) set(dec Decision) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last = &dec
	d.all = append(d.all, dec)
	if len(d.all) > 50 {
		d.all = d.all[len(d.all)-50:]
	}
}

func (d *decisionStore) snapshot() (*Decision, []Decision) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Decision, len(d.all))
	for i, r := range d.all {
		out[len(d.all)-1-i] = r // newest first
	}
	return d.last, out
}

// postDecision records a next-step choice from the /decide page.
func (s *Server) postDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Choice string `json:"choice"`
		Label  string `json:"label"`
		Custom string `json:"custom"`
		Note   string `json:"note"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Choice == "" {
		writeError(w, http.StatusUnprocessableEntity, "choice required")
		return
	}
	body.Choice = clamp(body.Choice, 32)
	body.Label = clamp(body.Label, 120)
	body.Custom = clamp(body.Custom, 500)
	body.Note = clamp(body.Note, 1000)
	s.decisions.set(Decision{
		Choice:     body.Choice,
		Label:      body.Label,
		Custom:     body.Custom,
		Note:       body.Note,
		ReceivedAt: time.Now(),
	})
	s.log.Info("next-step decision", "choice", body.Choice, "label", body.Label, "custom", body.Custom, "note", body.Note)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// getDecision returns the latest choice plus recent history.
func (s *Server) getDecision(w http.ResponseWriter, r *http.Request) {
	last, all := s.decisions.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"latest": last, "history": all})
}
