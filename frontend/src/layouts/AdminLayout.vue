<script setup lang="ts">
import { ref, computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import ToastHost from '@/components/ui/ToastHost.vue'
import { usePoll } from '@/composables/usePoll'
import { getCollisions, getOverstays, getNotifications } from '@/api/client'
import { getAdminEmail, logout } from '@/api/auth'

const route = useRoute()
const menuOpen = ref(false)
const alertCount = ref(0)
const noteCount = ref(0)
const connected = ref(true)
const adminEmail = getAdminEmail()

const nav = computed(() => [
  { name: 'dashboard', to: '/', label: 'Dashboard', icon: 'grid', badge: alertCount.value, badgeTone: 'red' },
  { name: 'reservations', to: '/reservations', label: 'Reservations', icon: 'calendar', badge: 0, badgeTone: '' },
  { name: 'rooms', to: '/rooms', label: 'Rooms', icon: 'door', badge: 0, badgeTone: '' },
  { name: 'users', to: '/users', label: 'Users', icon: 'person', badge: 0, badgeTone: '' },
  { name: 'notifications', to: '/notifications', label: 'Notifications', icon: 'bell', badge: noteCount.value, badgeTone: 'gray' },
])

async function pollBadges() {
  try {
    const [collisions, overstays, notes] = await Promise.all([
      getCollisions(), getOverstays(), getNotifications(200),
    ])
    alertCount.value = collisions.length + overstays.length
    noteCount.value = notes.length
    connected.value = true
  } catch {
    connected.value = false
  }
}
usePoll(pollBadges, 10000)
</script>

<template>
  <div class="shell">
    <button class="menu-btn" aria-label="Menu" @click="menuOpen = !menuOpen">
      <span /><span /><span />
    </button>

    <aside :class="{ open: menuOpen }">
      <div class="logo">
        <span class="beacon"><i /></span>
        <span class="wordmark">Quick<b>Room</b></span>
      </div>

      <nav>
        <RouterLink
          v-for="item in nav"
          :key="item.name"
          :to="item.to"
          class="nav-item"
          :class="{ active: route.name === item.name || (item.name === 'rooms' && route.name === 'room-detail') }"
          @click="menuOpen = false"
        >
          <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true">
            <template v-if="item.icon === 'grid'">
              <rect x="1.5" y="1.5" width="5.4" height="5.4" rx="1.4" /><rect x="9.1" y="1.5" width="5.4" height="5.4" rx="1.4" />
              <rect x="1.5" y="9.1" width="5.4" height="5.4" rx="1.4" /><rect x="9.1" y="9.1" width="5.4" height="5.4" rx="1.4" />
            </template>
            <template v-else-if="item.icon === 'calendar'">
              <rect x="1.75" y="2.75" width="12.5" height="11.5" rx="2" fill="none" stroke-width="1.5" />
              <line x1="1.75" y1="6.4" x2="14.25" y2="6.4" stroke-width="1.5" />
              <line x1="5" y1="1" x2="5" y2="3.6" stroke-width="1.5" stroke-linecap="round" />
              <line x1="11" y1="1" x2="11" y2="3.6" stroke-width="1.5" stroke-linecap="round" />
            </template>
            <template v-else-if="item.icon === 'door'">
              <rect x="3.25" y="1.75" width="9.5" height="12.5" rx="1.6" fill="none" stroke-width="1.5" />
              <circle cx="10.4" cy="8.2" r="1" />
            </template>
            <template v-else-if="item.icon === 'wave'">
              <circle cx="8" cy="8" r="1.6" />
              <path d="M4.6 11.4a4.8 4.8 0 0 1 0-6.8M11.4 4.6a4.8 4.8 0 0 1 0 6.8" fill="none" stroke-width="1.5" stroke-linecap="round" />
              <path d="M2.5 13.5a7.8 7.8 0 0 1 0-11M13.5 2.5a7.8 7.8 0 0 1 0 11" fill="none" stroke-width="1.5" stroke-linecap="round" />
            </template>
            <template v-else-if="item.icon === 'person'">
              <circle cx="8" cy="5" r="3" fill="none" stroke-width="1.5" />
              <path d="M2.4 14.2a5.8 5.8 0 0 1 11.2 0" fill="none" stroke-width="1.5" stroke-linecap="round" />
            </template>
            <template v-else>
              <path d="M8 1.8a4.3 4.3 0 0 1 4.3 4.3c0 3 .9 4.2 1.6 4.9H2.1c.7-.7 1.6-1.9 1.6-4.9A4.3 4.3 0 0 1 8 1.8z" fill="none" stroke-width="1.5" stroke-linejoin="round" />
              <path d="M6.6 13.6a1.5 1.5 0 0 0 2.8 0" fill="none" stroke-width="1.5" stroke-linecap="round" />
            </template>
          </svg>
          <span class="label">{{ item.label }}</span>
          <span v-if="item.badge" class="count" :class="item.badgeTone">{{ item.badge }}</span>
        </RouterLink>
      </nav>

      <footer>
        <span class="chip" :class="connected ? 'live' : 'down'">
          <span class="led" />{{ connected ? 'Live' : 'Reconnecting' }}
        </span>
        <div class="me">
          <div class="email">{{ adminEmail || 'Admin' }}</div>
          <button class="signout" @click="logout()">Sign out</button>
        </div>
      </footer>
    </aside>

    <div v-if="menuOpen" class="scrim" @click="menuOpen = false" />

    <main>
      <RouterView />
    </main>

    <ToastHost />
  </div>
</template>

<style scoped>
.shell { display: grid; grid-template-columns: 232px 1fr; min-height: 100vh; }
/* Fixed, not sticky: Safari drops stickiness on backdrop-filtered elements,
   which let the sidebar scroll away on long lists. The grid's first column
   stays as the spacer. */
aside { position: fixed; top: 0; left: 0; bottom: 0; width: 232px; display: flex; flex-direction: column;
  overflow-y: auto; background: rgba(255, 255, 255, .72); backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px);
  border-right: 1px solid var(--line); padding: 18px 12px 14px; z-index: 40; }
.logo { display: flex; align-items: center; gap: 9px; padding: 2px 10px 16px; }
.beacon { position: relative; width: 10px; height: 10px; flex: none; }
.beacon i { position: absolute; inset: 0; margin: auto; width: 8px; height: 8px; border-radius: 50%;
  background: #2fe6b0; box-shadow: 0 0 8px rgba(47, 230, 176, .8); }
.wordmark { font-family: var(--f-display); font-size: 16px; font-weight: 700; letter-spacing: -0.01em; }
.wordmark b { color: var(--accent); }
nav { display: grid; gap: 2px; }
.nav-item { display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 9px;
  color: var(--text); font-size: 13px; font-weight: 500; transition: background .16s ease; }
.nav-item:hover { background: rgba(0, 0, 0, .04); }
.nav-item.active { background: var(--accent-dim); color: var(--accent); }
.glyph { width: 16px; height: 16px; flex: none; fill: currentColor; stroke: currentColor; opacity: .85; }
.count { margin-left: auto; font-size: 10.5px; font-weight: 600; padding: 1px 7px; border-radius: 999px;
  font-variant-numeric: tabular-nums; }
.count.red { background: var(--danger); color: #fff; }
.count.gray { background: rgba(0, 0, 0, .08); color: var(--muted); }
footer { margin-top: auto; display: grid; gap: 10px; padding: 12px 10px 0; border-top: 1px solid var(--line-soft); }
.chip { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--muted);
  font-variant-numeric: tabular-nums; }
.chip .led { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); }
.chip.live .led { background: var(--signal); box-shadow: 0 0 6px rgba(52, 199, 89, .7); }
.chip.down { color: var(--amber); }
.chip.down .led { background: var(--amber); }
.me { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.email { font-size: 12px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.signout { background: none; border: none; color: var(--accent); font-size: 12px; font-weight: 500;
  cursor: pointer; padding: 2px 0; flex: none; font-family: var(--f-body); }
/* The fixed aside is out of flow, so main must be pinned to the content
   column explicitly or the grid auto-places it into the 232px sidebar track. */
main { grid-column: 2; padding: 26px 30px 60px; max-width: 1180px; width: 100%; margin: 0 auto; min-width: 0; }
.menu-btn { display: none; }
.scrim { display: none; }

@media (max-width: 760px) {
  .shell { grid-template-columns: 1fr; }
  aside { position: fixed; left: 0; top: 0; bottom: 0; width: 250px; transform: translateX(-100%);
    transition: transform .22s ease; background: rgba(255, 255, 255, .92); }
  aside.open { transform: translateX(0); }
  .scrim { display: block; position: fixed; inset: 0; background: rgba(0, 0, 0, .25); z-index: 30; }
  .menu-btn { display: grid; gap: 3.5px; position: fixed; top: 14px; left: 14px; z-index: 50;
    background: var(--panel); border: 1px solid var(--line); border-radius: 9px; padding: 9px 8px; cursor: pointer; }
  .menu-btn span { width: 15px; height: 1.6px; background: var(--text); border-radius: 2px; }
  main { grid-column: 1; padding: 60px 16px 40px; }
}
</style>
