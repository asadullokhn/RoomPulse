<script setup lang="ts">
import { ref } from 'vue'
import type { Beacon, Room } from '@/api/types'
import { putBeacon, deleteBeacon } from '@/api/client'

defineProps<{ beacons: Beacon[]; rooms: Room[] }>()
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
