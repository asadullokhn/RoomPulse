# Beacon CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin create, read, update, and delete the room↔beacon (iBeacon UUID/major/minor) mapping via the HTTP API and a new Admin UI section, with every mutation persisted to `BeaconsFile`.

**Architecture:** Two new routes (`PUT`/`DELETE /beacons/{workspace_id}`) alongside the existing `GET /beacons`, backed by one new `store.Memory` method (`RemoveBeacon`) and the already-written-but-unused `store.SaveBeacons`. A new `beacons.go` in `internal/api` consolidates all beacon handlers (moved out of `server.go`). Frontend gets a new `BeaconsPanel.vue` section in the Admin view.

**Tech Stack:** Go 1.26 (existing stack, no new dependencies), Vue 3 + TypeScript (existing frontend stack).

## Global Constraints

- Keep the existing one-beacon-per-workspace data model — no re-architecture (per spec's non-goals).
- Every mutation validates `workspace_id` against an existing room (404 if unknown) — mirrors the existing `createReservation` pattern.
- `uuid` non-empty, ≤128 chars (reuse `maxIDLen`); `major`/`minor` in `[0, 65535]` (iBeacon's actual 16-bit field width).
- Persist-to-disk failures are logged, not fatal to the request (best-effort, matches `upsertReservation`'s SQLite-write philosophy).
- No auth/role changes — this admin panel stays unauthenticated-by-design, matching every other admin endpoint.

---

## File Structure

```
backend/internal/store/beacons.go        — modify: add RemoveBeacon
backend/internal/store/beacons_test.go   — new
backend/internal/api/beacons.go          — new: beaconView, toBeaconView, listBeacons (moved from server.go), putBeacon, deleteBeacon
backend/internal/api/beacons_test.go     — new
backend/internal/api/server.go           — modify: remove old listBeacons, add beaconsFile field + ConfigureBeaconsFile, register 2 new routes
backend/cmd/quickroom/main.go            — modify: call ConfigureBeaconsFile
backend/internal/api/openapi.yaml        — modify: document PUT/DELETE /beacons/{workspace_id}
frontend/src/api/types.ts                — modify: add BeaconEntry fields consistency (already has Beacon-shaped type — verify/extend)
frontend/src/api/client.ts               — modify: add putBeacon, deleteBeacon
frontend/src/components/admin/BeaconsPanel.vue — new
frontend/src/views/AdminView.vue         — modify: add Beacons section
```

---

### Task 1: `store.Memory.RemoveBeacon`

**Files:**
- Modify: `backend/internal/store/beacons.go`
- Test: `backend/internal/store/beacons_test.go` (new)

**Interfaces:**
- Produces: `func (m *Memory) RemoveBeacon(workspaceID string)`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/store/beacons_test.go`:

```go
package store

import (
	"testing"

	"quickroom/internal/domain"
)

func TestSetAndRemoveBeacon(t *testing.T) {
	m := NewMemory()
	m.SetBeacon(domain.Beacon{WorkspaceID: "ws-1", UUID: "11111111-2222-3333-4444-555555555555", Major: 1, Minor: 101})

	if _, ok := m.Beacon("ws-1"); !ok {
		t.Fatal("beacon not found after SetBeacon")
	}

	m.RemoveBeacon("ws-1")

	if _, ok := m.Beacon("ws-1"); ok {
		t.Fatal("beacon still present after RemoveBeacon")
	}

	// Removing a non-existent beacon is a no-op, not an error.
	m.RemoveBeacon("ws-does-not-exist")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/... -run TestSetAndRemoveBeacon -v`
Expected: FAIL to compile — `m.RemoveBeacon undefined`.

- [ ] **Step 3: Add `RemoveBeacon` to `backend/internal/store/beacons.go`**

Append after the existing `SetBeacon` method:

```go
// RemoveBeacon deletes the iBeacon mapping for a workspace, if present.
func (m *Memory) RemoveBeacon(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.beacons, workspaceID)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/... -run TestSetAndRemoveBeacon -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/beacons.go backend/internal/store/beacons_test.go
git commit -m "Add store.Memory.RemoveBeacon"
```

---

### Task 2: `PUT`/`DELETE /beacons/{workspace_id}` API

**Files:**
- Create: `backend/internal/api/beacons.go`
- Test: `backend/internal/api/beacons_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/cmd/quickroom/main.go`

**Interfaces:**
- Consumes: `store.Memory.SetBeacon` (existing), `store.Memory.RemoveBeacon` (Task 1), `store.Memory.Beacons` (existing), `store.SaveBeacons` (existing, `backend/internal/store/persist.go`).
- Produces: `func (s *Server) ConfigureBeaconsFile(path string)`, routes `PUT /beacons/{workspace_id}`, `DELETE /beacons/{workspace_id}`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/beacons_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPutBeaconCreatesAndUpdates(t *testing.T) {
	h := newTestHandler(t)

	// ws-agung is one of the mock seed's built-in rooms.
	body, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 999})
	req := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	var created struct {
		WorkspaceID string `json:"workspace_id"`
		Minor       int    `json:"minor"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Minor != 999 || created.Name == "" {
		t.Fatalf("PUT response = %s", rec.Body.String())
	}

	// Update: same workspace, different minor.
	body2, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 888})
	req2 := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT (update) status = %d, want 200", rec2.Code)
	}
	var updated struct {
		Minor int `json:"minor"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &updated)
	if updated.Minor != 888 {
		t.Fatalf("PUT (update) minor = %d, want 888", updated.Minor)
	}
}

func TestPutBeaconUnknownWorkspace404s(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 1})
	req := httptest.NewRequest(http.MethodPut, "/beacons/ws-does-not-exist", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPutBeaconValidation(t *testing.T) {
	h := newTestHandler(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty uuid", map[string]any{"uuid": "", "major": 1, "minor": 1}},
		{"major too large", map[string]any{"uuid": "x", "major": 70000, "minor": 1}},
		{"minor negative", map[string]any{"uuid": "x", "major": 1, "minor": -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, _ := json.Marshal(c.body)
			req := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: status = %d, want 422", c.name, rec.Code)
			}
		})
	}
}

func TestDeleteBeacon(t *testing.T) {
	h := newTestHandler(t)

	putBody, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 1})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(putBody)))

	delReq := httptest.NewRequest(http.MethodDelete, "/beacons/ws-agung", nil)
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", delRec.Code)
	}

	// Deleting again (already gone) 404s.
	delReq2 := httptest.NewRequest(http.MethodDelete, "/beacons/ws-agung", nil)
	delRec2 := httptest.NewRecorder()
	h.ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want 404", delRec2.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run 'TestPutBeacon|TestDeleteBeacon' -v`
Expected: FAIL — 404s across the board (no `PUT`/`DELETE /beacons/{workspace_id}` route registered yet), so `TestPutBeaconUnknownWorkspace404s` spuriously passes but the others fail on status/shape assertions.

- [ ] **Step 3: Create `backend/internal/api/beacons.go`**

Move `listBeacons` here from `server.go` (delete it there in Step 4) and add the new handlers:

```go
package api

import (
	"net/http"

	"quickroom/internal/domain"
	"quickroom/internal/store"
)

const (
	maxUUIDLen  = maxIDLen // reuse the existing 128-char id bound
	maxBeaconID = 65535    // iBeacon major/minor are 16-bit unsigned
)

// beaconView is a beacon entry joined to its room name — the shape returned
// by GET, PUT, and (implicitly, via the deleted resource) DELETE /beacons.
type beaconView struct {
	WorkspaceID string `json:"workspace_id"`
	UUID        string `json:"uuid"`
	Major       int    `json:"major"`
	Minor       int    `json:"minor"`
	Name        string `json:"name"`
}

func (s *Server) toBeaconView(b domain.Beacon) beaconView {
	name := ""
	if room, ok := s.store.RoomByWorkspace(b.WorkspaceID); ok {
		name = room.Name
	}
	return beaconView{WorkspaceID: b.WorkspaceID, UUID: b.UUID, Major: b.Major, Minor: b.Minor, Name: name}
}

// listBeacons returns the room↔iBeacon registry, each entry joined to its room
// name. The mobile app polls this to learn which beacons to range/monitor, so
// rooms can be added or re-mapped without shipping a new build.
func (s *Server) listBeacons(w http.ResponseWriter, _ *http.Request) {
	bs := s.store.Beacons()
	out := make([]beaconView, 0, len(bs))
	for _, b := range bs {
		out = append(out, s.toBeaconView(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"beacons": out})
}

// persistBeacons best-effort writes the full current beacon registry to
// BeaconsFile so admin edits survive a restart. Logged, not fatal, on failure
// — the in-memory state (already applied by the caller) is authoritative for
// the running process either way.
func (s *Server) persistBeacons() {
	if s.beaconsFile == "" {
		return
	}
	if err := store.SaveBeacons(s.beaconsFile, s.store.Beacons()); err != nil {
		s.log.Warn("persist beacons", "err", err)
	}
}

// putBeacon creates or replaces the beacon mapping for a room (idempotent
// upsert — the same operation whether or not one already existed, so there's
// no separate POST-for-create route).
func (s *Server) putBeacon(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	if _, ok := s.store.RoomByWorkspace(workspaceID); !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	var body struct {
		UUID  string `json:"uuid"`
		Major int    `json:"major"`
		Minor int    `json:"minor"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.UUID == "" || len(body.UUID) > maxUUIDLen {
		writeError(w, http.StatusUnprocessableEntity, "uuid required; 1..128 chars")
		return
	}
	if body.Major < 0 || body.Major > maxBeaconID || body.Minor < 0 || body.Minor > maxBeaconID {
		writeError(w, http.StatusUnprocessableEntity, "major and minor must be 0..65535")
		return
	}

	b := domain.Beacon{WorkspaceID: workspaceID, UUID: body.UUID, Major: body.Major, Minor: body.Minor}
	s.store.SetBeacon(b)
	s.persistBeacons()
	writeJSON(w, http.StatusOK, s.toBeaconView(b))
}

// deleteBeacon removes a room's beacon mapping. 404 if none exists.
func (s *Server) deleteBeacon(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	if _, ok := s.store.Beacon(workspaceID); !ok {
		writeError(w, http.StatusNotFound, "beacon not found")
		return
	}
	s.store.RemoveBeacon(workspaceID)
	s.persistBeacons()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Remove the old `listBeacons` from `backend/internal/api/server.go`**

Delete this block (currently around line 410-428):

```go
func (s *Server) listBeacons(w http.ResponseWriter, _ *http.Request) {
	type entry struct {
		WorkspaceID string `json:"workspace_id"`
		UUID        string `json:"uuid"`
		Major       int    `json:"major"`
		Minor       int    `json:"minor"`
		Name        string `json:"name"`
	}
	bs := s.store.Beacons()
	out := make([]entry, 0, len(bs))
	for _, b := range bs {
		name := ""
		if room, ok := s.store.RoomByWorkspace(b.WorkspaceID); ok {
			name = room.Name
		}
		out = append(out, entry{WorkspaceID: b.WorkspaceID, UUID: b.UUID, Major: b.Major, Minor: b.Minor, Name: name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"beacons": out})
}
```

- [ ] **Step 5: Add the `beaconsFile` field, `ConfigureBeaconsFile` setter, and route registrations in `backend/internal/api/server.go`**

Add to the `Server` struct (after the `sessionTTL` field from the auth work, or — if this branch doesn't have that yet — after `overstayGrace`):

```go
	// BeaconsFile path for persisting admin beacon-mapping edits. Empty
	// disables persistence (in-memory only) — set via ConfigureBeaconsFile.
	beaconsFile string
```

Add a new setter, next to `ConfigureOverstay`:

```go
// ConfigureBeaconsFile sets the path admin beacon-mapping edits persist to.
// Empty disables persistence (edits apply in-memory only for the process's
// lifetime).
func (s *Server) ConfigureBeaconsFile(path string) {
	s.beaconsFile = path
}
```

In `Handler()`, add the two new routes next to the existing `GET /beacons`:

```go
	mux.HandleFunc("GET /beacons", s.listBeacons)
	mux.HandleFunc("PUT /beacons/{workspace_id}", s.putBeacon)
	mux.HandleFunc("DELETE /beacons/{workspace_id}", s.deleteBeacon)
```

(If `GET /beacons` isn't contiguous with other routes in the current file, just add the two new lines directly below wherever `GET /beacons` already is registered.)

- [ ] **Step 6: Wire `ConfigureBeaconsFile` in `backend/cmd/quickroom/main.go`**

Next to the other `apiSrv.Configure*` calls:

```go
	apiSrv.ConfigureBeaconsFile(cfg.BeaconsFile)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go build ./... && go vet ./... && go test ./... -v 2>&1 | tail -60`
Expected: all packages pass, including the 4 new beacon tests.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/beacons.go backend/internal/api/beacons_test.go backend/internal/api/server.go backend/cmd/quickroom/main.go
git commit -m "Add PUT/DELETE /beacons/{workspace_id} — full beacon CRUD, persisted to BeaconsFile"
```

---

### Task 3: `BeaconsPanel.vue` — Admin UI

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/components/admin/BeaconsPanel.vue`
- Modify: `frontend/src/views/AdminView.vue`

**Interfaces:**
- Consumes: existing `Beacon` type in `types.ts` (verify it already matches `beaconView`'s shape — it does, from the original frontend build: `{workspace_id, uuid, major, minor, name}`).
- Produces: `putBeacon(workspaceId: string, body: {uuid: string; major: number; minor: number}): Promise<Beacon>`, `deleteBeacon(workspaceId: string): Promise<void>` in `api/client.ts`.

- [ ] **Step 1: Add `putBeacon`/`deleteBeacon` to `frontend/src/api/client.ts`**

Add near the existing `getBeacons`:

```ts
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
```

Add `Beacon` to the existing `import type { ... } from './types'` line at the top of the file if not already imported.

- [ ] **Step 2: Verify `frontend/src/api/types.ts` already has a matching `Beacon` type**

It should already read:

```ts
export interface Beacon {
  workspace_id: string
  uuid: string
  major: number
  minor: number
  name: string
}
```

If it doesn't match exactly (e.g., missing `name`), fix it to match `beaconView`'s JSON shape from Task 2.

- [ ] **Step 3: Write `frontend/src/components/admin/BeaconsPanel.vue`**

```vue
<script setup lang="ts">
import { ref } from 'vue'
import type { Beacon, Room } from '@/api/types'
import { putBeacon, deleteBeacon } from '@/api/client'

const props = defineProps<{ beacons: Beacon[]; rooms: Room[] }>()
const emit = defineEmits<{ changed: [] }>()

const editingWs = ref<string | null>(null)
const editUuid = ref('')
const editMajor = ref(0)
const editMinor = ref(0)
const busy = ref(false)
const error = ref('')

const newWs = ref('')
const newUuid = ref('')
const newMajor = ref(1)
const newMinor = ref(1)

function startEdit(b: Beacon) {
  editingWs.value = b.workspace_id
  editUuid.value = b.uuid
  editMajor.value = b.major
  editMinor.value = b.minor
  error.value = ''
}
function cancelEdit() {
  editingWs.value = null
}
async function saveEdit(workspaceId: string) {
  busy.value = true
  error.value = ''
  try {
    await putBeacon(workspaceId, { uuid: editUuid.value, major: editMajor.value, minor: editMinor.value })
    editingWs.value = null
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'save failed'
  } finally {
    busy.value = false
  }
}
async function remove(workspaceId: string) {
  if (!confirm('Remove this beacon mapping?')) return
  busy.value = true
  error.value = ''
  try {
    await deleteBeacon(workspaceId)
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'delete failed'
  } finally {
    busy.value = false
  }
}
async function addNew() {
  if (!newWs.value || !newUuid.value) return
  busy.value = true
  error.value = ''
  try {
    await putBeacon(newWs.value, { uuid: newUuid.value, major: newMajor.value, minor: newMinor.value })
    newWs.value = ''; newUuid.value = ''; newMajor.value = 1; newMinor.value = 1
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'create failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="scroll">
      <table>
        <thead><tr><th>Room</th><th>Workspace</th><th>UUID</th><th>Major</th><th>Minor</th><th></th></tr></thead>
        <tbody>
          <tr v-for="b in beacons" :key="b.workspace_id">
            <template v-if="editingWs === b.workspace_id">
              <td>{{ b.name || b.workspace_id }}</td>
              <td class="mono id">{{ b.workspace_id }}</td>
              <td><input v-model="editUuid" class="cell-input mono" /></td>
              <td><input v-model.number="editMajor" type="number" min="0" max="65535" class="cell-input num" /></td>
              <td><input v-model.number="editMinor" type="number" min="0" max="65535" class="cell-input num" /></td>
              <td class="actions">
                <button class="btn-ghost" :disabled="busy" @click="saveEdit(b.workspace_id)">Save</button>
                <button class="btn-ghost" :disabled="busy" @click="cancelEdit">Cancel</button>
              </td>
            </template>
            <template v-else>
              <td>{{ b.name || b.workspace_id }}</td>
              <td class="mono id">{{ b.workspace_id }}</td>
              <td class="mono">{{ b.uuid }}</td>
              <td class="num">{{ b.major }}</td>
              <td class="num">{{ b.minor }}</td>
              <td class="actions">
                <button class="btn-ghost" @click="startEdit(b)">Edit</button>
                <button class="btn-ghost" :disabled="busy" @click="remove(b.workspace_id)">Delete</button>
              </td>
            </template>
          </tr>
          <tr v-if="!beacons.length"><td colspan="6" class="empty">No beacons registered.</td></tr>
          <tr class="add-row">
            <td colspan="2">
              <select v-model="newWs" class="cell-input">
                <option value="" disabled>Assign a room…</option>
                <option v-for="r in rooms" :key="r.zoom_workspace_id" :value="r.zoom_workspace_id">{{ r.name }}</option>
              </select>
            </td>
            <td><input v-model="newUuid" placeholder="uuid" class="cell-input mono" /></td>
            <td><input v-model.number="newMajor" type="number" min="0" max="65535" class="cell-input num" /></td>
            <td><input v-model.number="newMinor" type="number" min="0" max="65535" class="cell-input num" /></td>
            <td class="actions"><button class="btn-primary" :disabled="busy || !newWs || !newUuid" @click="addNew">Add</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="error" class="err">{{ error }}</div>
  </div>
</template>

<style scoped>
.cell-input { width: 100%; background: rgba(150,170,220,.06); border: 1px solid var(--line); border-radius: 6px;
  padding: 5px 8px; color: var(--text); font-family: var(--f-body); font-size: 13px; }
.cell-input.mono { font-family: var(--f-mono); }
.cell-input.num { width: 80px; }
.actions { display: flex; gap: 6px; white-space: nowrap; }
button { font-family: var(--f-body); font-size: 12px; font-weight: 500; cursor: pointer;
  border-radius: 8px; padding: 6px 11px; border: 1px solid transparent; }
.btn-ghost { background: transparent; color: var(--text); border-color: var(--line); }
.btn-ghost:hover { border-color: var(--accent); }
.btn-ghost:disabled { opacity: .5; cursor: default; }
.btn-primary { background: var(--accent); color: #0a0f1f; font-weight: 600; }
.btn-primary:disabled { opacity: .5; cursor: default; }
.add-row td { padding-top: 10px; }
.err { padding: 10px 16px; color: var(--danger); font-size: 12.5px; border-top: 1px solid var(--line-soft); }
</style>
```

- [ ] **Step 4: Wire `BeaconsPanel` into `frontend/src/views/AdminView.vue`**

Add the import, add `beacons`/`rooms` refs are already present as `rooms` (existing) — add a new `beacons` ref, fetch it in `refresh()`, and render the new section. Show the exact diff:

Add to the imports:
```ts
import BeaconsPanel from '@/components/admin/BeaconsPanel.vue'
import { getBeacons } from '@/api/client'
import type { Beacon } from '@/api/types'
```

Add a new ref next to the existing `rooms` ref:
```ts
const beacons = ref<Beacon[]>([])
```

In `refresh()`, add `getBeacons()` to the `Promise.all([...])` call and assign its result — show the full updated function:

```ts
async function refresh() {
  try {
    const [u, res, r, occ, col, over, notes, beac] = await Promise.all([
      getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(30), getBeacons(),
    ])
    util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
    collisions.value = col; overstays.value = over; notifications.value = notes; beacons.value = beac
    markUp(); loaded.value = true
  } catch {
    markDown()
  }
}
```

Add a new section in the template, after the Notification outbox section:

```html
<section class="block">
  <div class="eyebrow"><span class="n">05</span> Beacons <span class="aside">room ↔ iBeacon assignment</span></div>
  <BeaconsPanel :beacons="beacons" :rooms="rooms" @changed="refresh" />
</section>
```

- [ ] **Step 5: Build and typecheck**

Run: `cd frontend && npm run build`
Expected: exits 0.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/api/types.ts frontend/src/components/admin/BeaconsPanel.vue frontend/src/views/AdminView.vue
git commit -m "Add BeaconsPanel: assign/edit/delete room-beacon mappings from the Admin UI"
```

---

### Task 4: OpenAPI docs

**Files:**
- Modify: `backend/internal/api/openapi.yaml`

- [ ] **Step 1: Add `PUT`/`DELETE /beacons/{workspace_id}` paths**

Add near the existing `/beacons` GET entry:

```yaml
  /beacons/{workspace_id}:
    put:
      tags: [Rooms & Beacons]
      summary: Assign or update a room's beacon mapping
      parameters:
        - name: workspace_id
          in: path
          required: true
          schema: { type: string }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [uuid, major, minor]
              properties:
                uuid: { type: string }
                major: { type: integer, minimum: 0, maximum: 65535 }
                minor: { type: integer, minimum: 0, maximum: 65535 }
      responses:
        "200": { description: Saved, content: { application/json: { schema: { $ref: '#/components/schemas/BeaconEntry' } } } }
        "404": { $ref: '#/components/responses/NotFound' }
        "422": { $ref: '#/components/responses/Unprocessable' }
    delete:
      tags: [Rooms & Beacons]
      summary: Remove a room's beacon mapping
      parameters:
        - name: workspace_id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200": { $ref: '#/components/responses/Ok' }
        "404": { $ref: '#/components/responses/NotFound' }
```

- [ ] **Step 2: Validate YAML**

Run: `cd backend && python3 -c "import yaml; yaml.safe_load(open('internal/api/openapi.yaml')); print('valid')"`
Expected: `valid`.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/openapi.yaml
git commit -m "Document PUT/DELETE /beacons/{workspace_id} in the OpenAPI spec"
```

---

### Task 5: Full verification pass

- [ ] **Step 1: Backend tests + vet**

Run: `cd backend && go vet ./... && go test ./... -v 2>&1 | tail -80`
Expected: all green, including all Task 1/2 beacon tests.

- [ ] **Step 2: Frontend build**

Run: `cd frontend && npm run build`
Expected: exits 0.

- [ ] **Step 3: Docker build**

Run: `cd backend && docker build -t quickroom-beacon-test .`
Expected: succeeds.

- [ ] **Step 4: Manual in-browser verification**

Run the backend (`DB_PATH=/tmp/quickroom-beacon-check.db go run ./cmd/quickroom`) and frontend dev server, open `/`. Confirm: the Beacons section lists the 10 seeded beacons with room names; editing one's minor and saving updates the table; adding a new mapping to a room works; deleting one works with a confirm prompt; after a backend restart (`DB_PATH` file preserved), edited mappings persist (since `BeaconsFile` defaults to `seed.json`/`beacons.json` locally — confirm the actual default path via `BEACONS_FILE` env var, or set one explicitly for the check: `BEACONS_FILE=/tmp/quickroom-beacons-check.json`).

- [ ] **Step 5: Clean up**

Remove test containers/images and temp files created during verification.
