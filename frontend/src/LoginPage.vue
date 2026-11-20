<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  pending: boolean
  error: string
}>()
const emit = defineEmits<{
  submit: [email: string, password: string]
}>()

const { t } = useI18n()
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
      <img class="login-logo" src="/brand/velmir-logo-light.svg" alt="Velmir" />
      <h1 id="login-title">{{ t('auth.signIn') }}</h1>
      <p class="login-intro">{{ t('auth.intro') }}</p>
      <div v-if="error" class="login-error" role="alert">{{ t(error) }}</div>
      <form class="login-form" @submit.prevent="submit">
        <label>
          <span>{{ t('auth.email') }}</span>
          <input v-model="email" type="text" name="username" autocomplete="username" required autofocus :disabled="pending" />
        </label>
        <label>
          <span>{{ t('auth.password') }}</span>
          <input v-model="password" type="password" name="password" autocomplete="current-password" required :disabled="pending" />
        </label>
        <button type="submit" class="primary-button" :disabled="pending || !email || !password">
          {{ pending ? t('auth.signingIn') : t('auth.signIn') }}
        </button>
      </form>
    </section>
  </main>
</template>
