<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '@/api/auth'

document.title = 'QuickRoom · Sign in'

const router = useRouter()
const email = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (!email.value || !password.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await login(email.value, password.value)
    router.push({ name: 'dashboard' })
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'login failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <form class="card panel" @submit.prevent="submit">
      <div class="brand">
        <span class="beacon"><i /></span>
        <h1>Quick<b>Room</b></h1>
      </div>
      <div class="sub">Sign in to manage rooms, bookings, and people.</div>

      <label>
        <span>Email</span>
        <input v-model.trim="email" class="field" type="email" autocomplete="username" autofocus />
      </label>
      <label>
        <span>Password</span>
        <input v-model="password" class="field" type="password" autocomplete="current-password" />
      </label>

      <div v-if="error" class="err">{{ error }}</div>

      <button type="submit" class="btn-primary big" :disabled="busy || !email || !password">
        {{ busy ? 'Signing in' : 'Sign in' }}
      </button>
    </form>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: var(--page); }
.panel { width: min(360px, 100%); display: grid; gap: 14px; padding: 28px 26px; }
.brand { display: flex; align-items: center; gap: 10px; }
.brand h1 { font-family: var(--f-display); font-size: 21px; font-weight: 700; margin: 0; letter-spacing: -0.02em; }
.brand h1 b { color: var(--accent); }
.beacon { position: relative; width: 10px; height: 10px; flex: none; }
.beacon i { position: absolute; inset: 0; margin: auto; width: 8px; height: 8px; border-radius: 50%;
  background: #2fe6b0; box-shadow: 0 0 8px rgba(47, 230, 176, .8); }
.sub { font-size: 13px; color: var(--muted); margin-top: -6px; }
label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.err { color: var(--danger); font-size: 12.5px; }
.big { padding: 10px; font-size: 14px; }
</style>
