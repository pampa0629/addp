<template>
  <main class="administrator-recovery">
    <el-card class="recovery-card">
      <h1>{{ t('system.iam.credentialRecovery.title') }}</h1>
      <el-alert v-if="done" :title="t('system.iam.credentialRecovery.complete')" type="success" :closable="false" />
      <el-form v-else-if="!enrollment" label-position="top" @submit.prevent="begin">
        <el-form-item :label="t('system.iam.credentialRecovery.proof')"><el-input v-model="proof" type="textarea" autocomplete="off" :rows="3" :disabled="busy" /></el-form-item>
        <el-button type="primary" :loading="busy" :disabled="!proof.trim()" native-type="submit">{{ t('system.iam.common.continue') }}</el-button>
      </el-form>
      <el-form v-else label-position="top" @submit.prevent="complete">
        <TOTPEnrollmentDetails :secret="enrollment.secret" :uri="enrollment.otpauth_uri" />
        <p>{{ t('system.iam.credentialRecovery.expires', { time: new Date(enrollment.expires_at).toLocaleString() }) }}</p>
        <el-form-item :label="t('system.iam.credentialRecovery.password')"><el-input v-model="password" type="password" show-password autocomplete="new-password" :disabled="busy" /></el-form-item>
        <el-form-item :label="t('system.iam.users.confirmPassword')"><el-input v-model="confirmPassword" type="password" show-password autocomplete="new-password" :disabled="busy" /></el-form-item>
        <el-form-item :label="t('system.iam.security.verificationCode')"><el-input v-model="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" :disabled="busy" /></el-form-item>
        <el-button type="primary" :loading="busy" :disabled="!canComplete" native-type="submit">{{ t('system.iam.credentialRecovery.submit') }}</el-button>
      </el-form>
      <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" />
      <a href="/login" target="_top">{{ t('system.iam.credentialRecovery.login') }}</a>
    </el-card>
  </main>
</template>
<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import axios from 'axios'
import TOTPEnrollmentDetails from '../components/iam/TOTPEnrollmentDetails.vue'
const { t, locale } = useI18n()
const proof = ref(''), password = ref(''), confirmPassword = ref(''), code = ref(''), errorMessage = ref('')
const enrollment = ref(null), done = ref(false), busy = ref(false)
const client = axios.create({ baseURL: '/api/v1/system/auth/administrator-recovery', timeout: 10000 })
const controller = new AbortController()
let alive = true
const canComplete = computed(() => [...password.value].length >= 14 && password.value === confirmPassword.value && /^\d{6}$/.test(code.value))
function options() { return { signal: controller.signal, headers: { 'Accept-Language': locale.value } } }
async function begin() {
  if (busy.value) return
  busy.value = true; errorMessage.value = ''
  try { const { data } = await client.post('/enrollments', { recovery_token: proof.value.trim() }, options()); if (alive) { enrollment.value = data; proof.value = '' } }
  catch (error) { if (alive) errorMessage.value = error.response?.data?.error || t('system.iam.credentialRecovery.failed') }
  finally { if (alive) busy.value = false }
}
async function complete() {
  if (!canComplete.value || busy.value) return
  busy.value = true; errorMessage.value = ''
  try { await client.post('/completions', { enrollment_token: enrollment.value.enrollment_token, password: password.value, code: code.value }, options()); if (alive) { done.value = true; enrollment.value = null; password.value = ''; confirmPassword.value = ''; code.value = '' } }
  catch (error) { if (alive) { code.value = ''; errorMessage.value = error.response?.data?.error || t('system.iam.credentialRecovery.failed') } }
  finally { if (alive) busy.value = false }
}
onBeforeUnmount(() => { alive = false; controller.abort(); proof.value = ''; password.value = ''; confirmPassword.value = ''; code.value = ''; enrollment.value = null })
</script>
<style scoped>
.administrator-recovery { min-height: 100vh; display: grid; place-items: center; padding: 24px; box-sizing: border-box; background: var(--addp-bg-secondary); }
.recovery-card { width: min(100%, 520px); }
h1 { font-size: 22px; margin-bottom: 20px; color: var(--addp-text-primary); }
p, a, .el-alert { margin-top: 16px; }
a { display: inline-block; color: var(--el-color-primary); }
</style>
