<script setup lang="ts">
import { ref, watch } from 'vue'
import { createRoom, patchRoom, putBeacon, deleteBeacon, getRooms } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Room, Beacon } from '@/api/types'
import Modal from '@/components/ui/Modal.vue'

// The fleet's shared iBeacon proximity UUID — rooms differ by major/minor.
const DEFAULT_BEACON_UUID = '11111111-2222-3333-4444-555555555555'

const props = defineProps<{
  open: boolean
  room: Room | null // null = add a new room
  beacon: Beacon | null
}>()
const emit = defineEmits<{ close: []; saved: [] }>()

const toast = useToast()
const formName = ref('')
const formCapacity = ref(4)
const formTv = ref(false)
const formBeaconUuid = ref('')
const formBeaconMajor = ref<number | null>(null)
const formBeaconMinor = ref<number | null>(null)
const formError = ref('')
const busy = ref(false)

function isCustom(ws: string) { return ws.startsWith('cr-') }

watch(() => props.open, (open) => {
  if (!open) return
  formName.value = props.room?.name ?? ''
  formCapacity.value = props.room?.capacity ?? 4
  formTv.value = props.room?.has_tv ?? false
  formBeaconUuid.value = props.beacon?.uuid ?? DEFAULT_BEACON_UUID
  formBeaconMajor.value = props.beacon?.major ?? null
  formBeaconMinor.value = props.beacon?.minor ?? null
  formError.value = ''
})

async function submitForm() {
  busy.value = true
  formError.value = ''
  try {
    let ws = props.room?.zoom_workspace_id ?? ''
    if (ws) {
      await patchRoom(ws, { name: formName.value, capacity: formCapacity.value, has_tv: formTv.value })
    } else {
      await createRoom({ name: formName.value, capacity: formCapacity.value, has_tv: formTv.value })
      // resolve the new room's workspace id by name to attach the beacon
      const fresh = await getRooms()
      ws = fresh.find(r => r.name === formName.value)?.zoom_workspace_id ?? ''
    }

    const hadBeacon = !!props.beacon
    const wantsBeacon = formBeaconMajor.value !== null && formBeaconMinor.value !== null && formBeaconUuid.value.trim() !== ''
    if (ws && wantsBeacon) {
      await putBeacon(ws, { uuid: formBeaconUuid.value.trim(), major: formBeaconMajor.value!, minor: formBeaconMinor.value! })
    } else if (ws && hadBeacon && !wantsBeacon) {
      await deleteBeacon(ws)
    }

    toast.success(props.room ? 'Room updated' : 'Room added')
    emit('saved')
    emit('close')
  } catch (e) {
    formError.value = e instanceof Error ? e.message : 'request failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Modal :title="room ? 'Edit room' : 'Add room'" :open="open" @close="emit('close')">
    <form class="form" @submit.prevent="submitForm">
      <label><span>Name</span><input v-model.trim="formName" class="field" required /></label>
      <label><span>Capacity</span><input v-model.number="formCapacity" class="field" type="number" min="0" required /></label>
      <label class="check"><input v-model="formTv" type="checkbox" /><span>Has a TV</span></label>

      <div class="beacon-block">
        <div class="bb-title">Beacon</div>
        <label><span>Proximity UUID</span><input v-model.trim="formBeaconUuid" class="field mono" placeholder="Proximity UUID" /></label>
        <div class="two">
          <label><span>Major</span><input v-model.number="formBeaconMajor" class="field" type="number" min="0" max="65535" placeholder="—" /></label>
          <label><span>Minor</span><input v-model.number="formBeaconMinor" class="field" type="number" min="0" max="65535" placeholder="—" /></label>
        </div>
        <p class="hint">Major + minor identify this room's beacon. Clear both to detach the beacon.</p>
      </div>

      <p v-if="room && !isCustom(room.zoom_workspace_id)" class="hint">
        This is a Zoom-synced room: your change is stored as an override and re-applied after every sync.
      </p>
      <div v-if="formError" class="ferr">{{ formError }}</div>
      <div class="factions">
        <button type="button" class="btn-secondary" @click="emit('close')">Cancel</button>
        <button type="submit" class="btn-primary" :disabled="busy || !formName">
          {{ room ? 'Save changes' : 'Add room' }}
        </button>
      </div>
    </form>
  </Modal>
</template>

<style scoped>
.form { display: grid; gap: 12px; }
.form label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.check { display: flex !important; flex-direction: row; align-items: center; gap: 8px; }
.check input { width: 15px; height: 15px; accent-color: var(--accent); }
.check span { font-size: 13px; color: var(--text); }
.beacon-block { border: 1px solid var(--line-soft); border-radius: 11px; padding: 12px; display: grid; gap: 10px; }
.bb-title { font-size: 12px; font-weight: 700; color: var(--muted); }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.hint { margin: 0; font-size: 12px; color: var(--faint); }
.ferr { color: var(--danger); font-size: 12.5px; }
.factions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.mono { font-variant-numeric: tabular-nums; }
@media (max-width: 560px) { .two { grid-template-columns: 1fr; } }
</style>
