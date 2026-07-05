<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getBeacons, getRooms, putBeacon, deleteBeacon } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Beacon, Room } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import Modal from '@/components/ui/Modal.vue'

document.title = 'QuickRoom · Beacons'

const toast = useToast()
const beacons = ref<Beacon[]>([])
const rooms = ref<Room[]>([])
const loaded = ref(false)
const search = ref('')
const busy = ref(false)

const editingWs = ref<string | null>(null)
const editUuid = ref('')
const editMajor = ref(0)
const editMinor = ref(0)

const newWs = ref('')
const newUuid = ref('')
const newMajor = ref(1)
const newMinor = ref(1)

const deleteTarget = ref<Beacon | null>(null)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return q ? beacons.value.filter(b => `${b.name} ${b.workspace_id}`.toLowerCase().includes(q)) : beacons.value
})
const unassigned = computed(() =>
  rooms.value.filter(r => !beacons.value.some(b => b.workspace_id === r.zoom_workspace_id)))

function startEdit(b: Beacon) {
  editingWs.value = b.workspace_id
  editUuid.value = b.uuid
  editMajor.value = b.major
  editMinor.value = b.minor
}

async function saveEdit(workspaceId: string) {
  busy.value = true
  try {
    await putBeacon(workspaceId, { uuid: editUuid.value, major: editMajor.value, minor: editMinor.value })
    toast.success('Beacon mapping saved')
    editingWs.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'save failed')
  } finally {
    busy.value = false
  }
}

async function addNew() {
  if (!newWs.value || !newUuid.value) return
  busy.value = true
  try {
    await putBeacon(newWs.value, { uuid: newUuid.value, major: newMajor.value, minor: newMinor.value })
    toast.success('Beacon assigned')
    newWs.value = ''; newMajor.value = 1; newMinor.value = 1
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'assign failed')
  } finally {
    busy.value = false
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  busy.value = true
  try {
    await deleteBeacon(deleteTarget.value.workspace_id)
    toast.success('Beacon mapping removed')
    deleteTarget.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'delete failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  const [b, r] = await Promise.all([getBeacons(), getRooms()])
  beacons.value = b; rooms.value = r
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Beacons</h1>
        <p class="sub">Which iBeacon identity marks which room. Phones range these to check people in.</p>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search rooms" />
      <div class="card scroll">
        <table>
          <thead>
            <tr><th>Room</th><th>Workspace</th><th>UUID</th><th>Major</th><th>Minor</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="b in filtered" :key="b.workspace_id">
              <template v-if="editingWs === b.workspace_id">
                <td class="strong">{{ b.name || b.workspace_id }}</td>
                <td class="mono">{{ b.workspace_id }}</td>
                <td><input v-model.trim="editUuid" class="field mono-field" /></td>
                <td><input v-model.number="editMajor" class="field small" type="number" min="0" max="65535" /></td>
                <td><input v-model.number="editMinor" class="field small" type="number" min="0" max="65535" /></td>
                <td class="actions">
                  <button class="btn-ghost" :disabled="busy" @click="saveEdit(b.workspace_id)">Save</button>
                  <button class="btn-ghost" @click="editingWs = null">Cancel</button>
                </td>
              </template>
              <template v-else>
                <td class="strong">{{ b.name || b.workspace_id }}</td>
                <td class="mono">{{ b.workspace_id }}</td>
                <td class="mono">{{ b.uuid }}</td>
                <td class="num">{{ b.major }}</td>
                <td class="num">{{ b.minor }}</td>
                <td class="actions">
                  <button class="btn-ghost" @click="startEdit(b)">Edit</button>
                  <button class="btn-danger-ghost" @click="deleteTarget = b">Remove</button>
                </td>
              </template>
            </tr>
            <tr class="add-row">
              <td colspan="2">
                <select v-model="newWs" class="field">
                  <option value="" disabled>Assign a room</option>
                  <option v-for="r in unassigned" :key="r.zoom_workspace_id" :value="r.zoom_workspace_id">{{ r.name }}</option>
                </select>
              </td>
              <td><input v-model.trim="newUuid" class="field mono-field" placeholder="Proximity UUID" /></td>
              <td><input v-model.number="newMajor" class="field small" type="number" min="0" max="65535" /></td>
              <td><input v-model.number="newMinor" class="field small" type="number" min="0" max="65535" /></td>
              <td class="actions">
                <button class="btn-primary" :disabled="busy || !newWs || !newUuid" @click="addNew()">Assign</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <Modal
      title="Remove this beacon mapping?"
      :open="deleteTarget !== null"
      variant="confirm"
      confirm-label="Remove mapping"
      danger
      :busy="busy"
      @close="deleteTarget = null"
      @confirm="confirmDelete"
    >
      <p class="confirm-text" v-if="deleteTarget">
        Phones stop checking people into {{ deleteTarget.name || deleteTarget.workspace_id }} until a new beacon is assigned.
      </p>
    </Modal>
  </div>
</template>

<style scoped>
.strong { font-weight: 600; }
.actions { text-align: right; white-space: nowrap; }
.mono-field { font-family: var(--f-mono); font-size: 12px; width: 100%; min-width: 240px; }
.small { width: 76px; }
.add-row td { background: var(--raised); }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
</style>
