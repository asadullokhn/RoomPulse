package api

import (
	"net/http"
	"sync"
	"time"
)

// ScenarioAnswer is one team member's chosen answer for a single scenario from
// the /scenarios catalog.
type ScenarioAnswer struct {
	Name       string    `json:"name"`
	ScenarioID string    `json:"scenario_id"`
	Choice     string    `json:"choice"`
	Custom     string    `json:"custom"`
	Note       string    `json:"note"`
	ReceivedAt time.Time `json:"received_at"`
}

// scenarioAnswerStore keeps the latest answer per (person, scenario), so each
// teammate's picks are preserved independently — one person submitting never
// overwrites another's. Kept in memory; read back via GET /scenario-answers.
type scenarioAnswerStore struct {
	mu sync.Mutex
	m  map[string]map[string]ScenarioAnswer // name -> scenario_id -> answer
}

func newScenarioAnswerStore() *scenarioAnswerStore {
	return &scenarioAnswerStore{m: map[string]map[string]ScenarioAnswer{}}
}

func (s *scenarioAnswerStore) set(a ScenarioAnswer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m[a.Name] == nil {
		s.m[a.Name] = map[string]ScenarioAnswer{}
	}
	s.m[a.Name][a.ScenarioID] = a
}

func (s *scenarioAnswerStore) snapshot() map[string]map[string]ScenarioAnswer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]map[string]ScenarioAnswer, len(s.m))
	for name, byScenario := range s.m {
		inner := make(map[string]ScenarioAnswer, len(byScenario))
		for k, v := range byScenario {
			inner[k] = v
		}
		out[name] = inner
	}
	return out
}

// postScenarioAnswers records the chosen answers for one or several submitters.
// Body shape: {"names":["Ali","Abu"],"answers":[{"scenario_id","choice","custom","note"}]}.
// ("name" is still accepted for a single submitter.) The same answers are stored
// under each name, so a group that agrees can submit together.
func (s *Server) postScenarioAnswers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string   `json:"name"`  // legacy single submitter
		Names   []string `json:"names"` // one or several submitters
		Answers []struct {
			ScenarioID string `json:"scenario_id"`
			Choice     string `json:"choice"`
			Custom     string `json:"custom"`
			Note       string `json:"note"`
		} `json:"answers"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	raw := body.Names
	if len(raw) == 0 && body.Name != "" {
		raw = []string{body.Name}
	}
	names := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, n := range raw {
		n = clamp(n, maxNameLen)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	if len(names) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	now := time.Now()
	perPerson := 0
	for _, name := range names {
		cnt := 0
		for _, a := range body.Answers {
			sid := clamp(a.ScenarioID, 64)
			if sid == "" {
				continue
			}
			s.scenarioAnswers.set(ScenarioAnswer{
				Name:       name,
				ScenarioID: sid,
				Choice:     clamp(a.Choice, 160),
				Custom:     clamp(a.Custom, 500),
				Note:       clamp(a.Note, 1000),
				ReceivedAt: now,
			})
			cnt++
		}
		perPerson = cnt
	}
	s.log.Info("scenario answers", "people", names, "answers", perPerson)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "people": len(names), "stored": perPerson})
}

// getScenarioAnswers returns every teammate's latest answers, grouped by name.
func (s *Server) getScenarioAnswers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"by_person": s.scenarioAnswers.snapshot()})
}
