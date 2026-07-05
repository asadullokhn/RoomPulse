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
    router.push({ name: 'admin' })
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'login failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <form class="card" @submit.prevent="submit">
      <div class="brand">
        <span class="beacon"><i /></span>
        <h1>Quick<b>Room</b></h1>
      </div>
      <div class="sub">Admin sign in</div>

      <label>
        <span>Email</span>
        <input v-model.trim="email" type="email" autocomplete="username" autofocus />
      </label>
      <label>
        <span>Password</span>
        <input v-model="password" type="password" autocomplete="current-password" />
      </label>

      <div v-if="error" class="err">{{ error }}</div>

      <button type="submit" :disabled="busy || !email || !password">
        {{ busy ? 'Signing in…' : 'Sign in' }}
      </button>
    </form>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
.card { width: min(360px, 100%); display: grid; gap: 14px; padding: 26px 24px;
  background: linear-gradient(180deg, var(--panel-2), var(--panel));
  border: 1px solid var(--line); border-radius: var(--r); }
.brand { display: flex; align-items: center; gap: 10px; }
.brand h1 { font-family: var(--f-display); font-size: 20px; font-weight: 700; margin: 0; }
.brand h1 b { color: #2FE6B0; }
.beacon { position: relative; width: 11px; height: 11px; flex: none; }
.beacon i { position: absolute; inset: 0; margin: auto; width: 9px; height: 9px; border-radius: 50%;
  background: #2FE6B0; box-shadow: 0 0 12px #2FE6B0; }
.sub { font-size: 12px; color: var(--muted); margin-top: -6px; }
label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); }
input { background: rgba(150,170,220,.05); border: 1px solid var(--line); border-radius: 9px;
  color: var(--ink); padding: 9px 11px; font-size: 14px; font-family: var(--f-body); }
input:focus { outline: none; border-color: var(--signal-line); }
.err { color: var(--danger, #ff6b6b); font-size: 12.5px; }
button { background: #2FE6B0; color: #06281e; border: none; border-radius: 9px; padding: 10px;
  font-weight: 700; font-size: 14px; cursor: pointer; font-family: var(--f-body); }
button:disabled { opacity: .55; cursor: default; }
</style>
