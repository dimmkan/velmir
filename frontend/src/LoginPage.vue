<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{
  pending: boolean
  error: string
}>()
const emit = defineEmits<{
  submit: [email: string, password: string]
}>()

const email = ref('')
const password = ref('')

function submit() {
  if (!email.value || !password.value || props.pending) return
  emit('submit', email.value, password.value)
  password.value = ''
}
</script>

<template>
  <main class="login-layout">
    <section class="login-panel" aria-labelledby="login-title">
      <p class="eyebrow">Velmir administration</p>
      <h1 id="login-title">Sign in</h1>
      <p class="login-intro">Use your Velmir administrator account to continue.</p>
      <div v-if="error" class="login-error" role="alert">{{ error }}</div>
      <form class="login-form" @submit.prevent="submit">
        <label>
          <span>Email</span>
          <input v-model="email" type="email" name="email" autocomplete="username" required autofocus :disabled="pending" />
        </label>
        <label>
          <span>Password</span>
          <input v-model="password" type="password" name="password" autocomplete="current-password" required :disabled="pending" />
        </label>
        <button type="submit" class="primary-button" :disabled="pending || !email || !password">
          {{ pending ? 'Signing in…' : 'Sign in' }}
        </button>
      </form>
    </section>
  </main>
</template>
