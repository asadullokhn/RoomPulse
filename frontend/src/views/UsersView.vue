<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getUsers, deleteUser, renameUser } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { User } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Modal from '@/components/ui/Modal.vue'

document.title = 'QuickRoom · Users'

const toast = useToast()
const users = ref<User[]>([])
const loaded = ref(false)
const search = ref('')
const page = ref(1)
const PER_PAGE = 25
const busy = ref(false)

const renamingId = ref<string | null>(null)
const renameValue = ref('')
const deleteTarget = ref<User | null>(null)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(u => `${u.name} ${u.email} ${u.user_id}`.toLowerCase().includes(q))
})
const paged = computed(() => filtered.value.slice((page.value - 1) * PER_PAGE, page.value * PER_PAGE))

function fmtJoined(s?: string) {
  return s ? new Date(s).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }) : '—'
}

// Rating: >=80 dependable, <50 gets the halved no-show grace.
function ratingTone(v: number) { return v >= 80 ? 'b-signal' : v >= 50 ? 'b-blue' : 'b-danger' }

function startRename(u: User) {
  renamingId.value = u.user_id
  renameValue.value = u.name || ''
}

async function saveRename(userId: string) {
  busy.value = true
  try {
    await renameUser(userId, renameValue.value)
    toast.success('Name updated')
    renamingId.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'rename failed')
  } finally {
    busy.value = false
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  busy.value = true
  try {
    await deleteUser(deleteTarget.value.user_id)
    toast.success('Account deleted')
    deleteTarget.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'delete failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  users.value = await getUsers()
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Users</h1>
        <p class="sub">{{ users.length }} accounts signed in with Apple. Ratings are admin-only.</p>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search name or email" @update:search="page = 1" />
      <div class="card scroll">
        <table>
          <thead>
            <tr><th>Name</th><th>Email</th><th>Rating</th><th>Showed up</th><th>No-shows</th><th>Joined</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="u in paged" :key="u.user_id">
              <td class="strong">
                <template v-if="renamingId === u.user_id">
                  <input v-model.trim="renameValue" class="field rename" @keyup.enter="saveRename(u.user_id)" />
                </template>
                <router-link v-else class="user-link" :to="`/users/${u.user_id}`">{{ u.name || '—' }}</router-link>
              </td>
              <td class="mutedc">{{ u.email || '—' }}</td>
              <td>
                <span v-if="u.rating" class="badge" :class="ratingTone(u.rating.effective)">
                  {{ u.rating.effective }}<template v-if="u.rating.override !== undefined"> &middot; pinned</template>
                </span>
              </td>
              <td class="mutedc">{{ u.rating?.good ?? 0 }}</td>
              <td class="mutedc">{{ u.rating?.bad ?? 0 }}</td>
              <td class="mutedc">{{ fmtJoined(u.created_at) }}</td>
              <td class="actions">
                <template v-if="renamingId === u.user_id">
                  <button class="btn-ghost" :disabled="busy || !renameValue" @click="saveRename(u.user_id)">Save</button>
                  <button class="btn-ghost" @click="renamingId = null">Cancel</button>
                </template>
                <template v-else>
                  <button class="btn-ghost" @click="startRename(u)">Rename</button>
                  <button class="btn-danger-ghost" :disabled="busy" @click="deleteTarget = u">Delete</button>
                </template>
              </td>
            </tr>
            <tr v-if="!paged.length">
              <td colspan="7" class="empty"><b>No accounts match.</b>Try a different search.</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination v-model:page="page" :total="filtered.length" :per-page="PER_PAGE" />
    </template>

    <Modal
      title="Delete this account?"
      :open="deleteTarget !== null"
      variant="confirm"
      confirm-label="Delete account"
      danger
      :busy="busy"
      @close="deleteTarget = null"
      @confirm="confirmDelete"
    >
      <p class="confirm-text" v-if="deleteTarget">
        {{ deleteTarget.name || deleteTarget.email || deleteTarget.user_id }} loses access immediately;
        their open bookings are cancelled.
      </p>
    </Modal>
  </div>
</template>

<style scoped>
.strong { font-weight: 600; }
.mutedc { color: var(--muted); }
.actions { text-align: right; white-space: nowrap; }
.user-link { color: inherit; text-decoration: none; }
.user-link:hover { text-decoration: underline; }
.rename { width: 150px; padding: 5px 8px; }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
</style>
