<script setup lang="ts">
import { ref } from 'vue'
import type { Room, OccupancyEntry } from '@/api/types'
import { createRoom, patchRoom, deleteRoom } from '@/api/client'

const props = defineProps<{ rooms: Room[]; occupancyByWs: Record<string, OccupancyEntry> }>()
const emit = defineEmits<{ changed: [] }>()

const busy = ref(false)
const error = ref('')
const editingWs = ref('')
const editName = ref('')
const editCapacity = ref(0)
const adding = ref(false)
const newName = ref('')
const newCapacity = ref(4)

function occCount(ws: string) { return props.occupancyByWs[ws]?.count ?? 0 }
function occUsers(ws: string) { return props.occupancyByWs[ws]?.users?.join(', ') ?? '' }
function isCustom(ws: string) { return ws.startsWith('cr-') }

function startEdit(rm: Room) {
  editingWs.value = rm.zoom_workspace_id
  editName.value = rm.name
  editCapacity.value = rm.capacity
  error.value = ''
}

async function run(action: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  try {
    await action()
    editingWs.value = ''
    adding.value = false
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'request failed'
  } finally {
    busy.value = false
  }
}

const saveEdit = (ws: string) => run(() => patchRoom(ws, { name: editName.value, capacity: editCapacity.value }))
const removeRoom = (ws: string) => run(() => deleteRoom(ws))
const addRoom = () => run(async () => {
  await createRoom({ name: newName.value, capacity: newCapacity.value, has_tv: false })
  newName.value = ''
  newCapacity.value = 4
})
</script>

<template>
  <div>
    <div v-if="error" class="err">{{ error }}</div>
    <div class="rooms">
      <div v-for="rm in rooms" :key="rm.zoom_workspace_id" class="room" :class="{ busy: occCount(rm.zoom_workspace_id) > 0 }">
        <template v-if="editingWs === rm.zoom_workspace_id">
          <input v-model.trim="editName" class="field" placeholder="Name" />
          <input v-model.number="editCapacity" class="field" type="number" min="0" placeholder="Capacity" />
          <div class="actions">
            <button class="btn-ghost" :disabled="busy || !editName" @click="saveEdit(rm.zoom_workspace_id)">Save</button>
            <button class="btn-ghost" @click="editingWs = ''">Cancel</button>
          </div>
        </template>
        <template v-else>
          <div class="rn">{{ rm.name }} <span class="dot" :class="{ on: occCount(rm.zoom_workspace_id) > 0 }" /></div>
          <div class="head"><span class="c">{{ occCount(rm.zoom_workspace_id) }}</span><span class="cap">/ {{ rm.capacity }} seats</span></div>
          <div class="who">{{ occUsers(rm.zoom_workspace_id) || 'empty' }}</div>
          <div class="actions">
            <span v-if="isCustom(rm.zoom_workspace_id)" class="src">custom</span>
            <button class="btn-ghost" @click="startEdit(rm)">Edit</button>
            <button class="btn-ghost" :disabled="busy" @click="removeRoom(rm.zoom_workspace_id)">
              {{ isCustom(rm.zoom_workspace_id) ? 'Delete' : 'Reset' }}
            </button>
          </div>
        </template>
      </div>

      <div class="room add">
        <template v-if="adding">
          <input v-model.trim="newName" class="field" placeholder="Room name" />
          <input v-model.number="newCapacity" class="field" type="number" min="0" placeholder="Capacity" />
          <div class="actions">
            <button class="btn-ghost" :disabled="busy || !newName" @click="addRoom()">Add</button>
            <button class="btn-ghost" @click="adding = false">Cancel</button>
          </div>
        </template>
        <button v-else class="add-btn" @click="adding = true; error = ''">+ Add room</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.rooms { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.room { background: linear-gradient(180deg, var(--panel-2), var(--panel)); border: 1px solid var(--line);
  border-radius: var(--r); padding: 14px 15px; }
.room.busy { border-color: var(--signal-line); }
.rn { font-weight: 600; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head { display: flex; align-items: baseline; gap: 8px; margin-top: 10px; }
.head .c { font-family: var(--f-display); font-size: 26px; font-weight: 700; line-height: 1; }
.room.busy .head .c { color: var(--signal); }
.head .cap { font-size: 12px; color: var(--muted); }
.who { margin-top: 9px; font-size: 12px; color: var(--muted); min-height: 16px; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); display: inline-block; }
.dot.on { background: var(--signal); box-shadow: 0 0 7px var(--signal); }
.actions { margin-top: 10px; display: flex; gap: 6px; align-items: center; }
.src { font-family: var(--f-mono); font-size: 10px; color: var(--faint); margin-right: auto; text-transform: uppercase; letter-spacing: .5px; }
.btn-ghost { background: none; border: 1px solid var(--line); border-radius: 8px; color: var(--muted);
  padding: 4px 10px; font-size: 11.5px; cursor: pointer; font-family: var(--f-body); }
.btn-ghost:hover { color: var(--ink); border-color: var(--signal-line); }
.field { width: 100%; background: rgba(150,170,220,.05); border: 1px solid var(--line); border-radius: 8px;
  color: var(--ink); padding: 7px 9px; font-size: 12.5px; font-family: var(--f-body); margin-bottom: 6px; }
.room.add { display: grid; place-items: stretch; }
.add-btn { background: none; border: 1px dashed var(--line); border-radius: var(--r); color: var(--muted);
  min-height: 96px; cursor: pointer; font-size: 13px; font-family: var(--f-body); }
.add-btn:hover { color: var(--ink); border-color: var(--signal-line); }
.err { color: var(--amber); font-size: 12px; margin-bottom: 8px; }
</style>
