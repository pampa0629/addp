<template>
  <el-dialog v-model="visible" :title="t('system.iam.security.stepUpTitle')" width="min(440px, calc(100% - 24px))" :close-on-click-modal="false">
    <el-form label-position="top" @submit.prevent="complete">
      <el-form-item :label="t('system.iam.security.verificationCode')">
        <el-input v-model="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" autofocus @keyup.enter="complete" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">{{ t('system.iam.common.cancel') }}</el-button>
      <el-button type="primary" :loading="submitting" :disabled="code.length !== 6" @click="complete">
        {{ t('system.iam.security.continueOperation') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const visible = ref(false)
const submitting = ref(false)
const code = ref('')
let active = null

function isCurrent(request) {
  return active === request && authStore.token === request.token && authStore.authContext === request.context
}

function request() {
  if (active || !authStore.token || !authStore.authContext) return Promise.resolve(false)
  return new Promise(resolve => {
    const pending = { resolve, token: authStore.token, context: authStore.authContext, challenge: null, controller: new AbortController() }
    active = pending
    iamAPI.mfa.beginStepUp(pending.controller.signal).then(challenge => {
      if (!isCurrent(pending)) return
      pending.challenge = challenge
      visible.value = true
    }).catch(error => {
      if (!isCurrent(pending)) return
      ElMessage.error(error.response?.data?.error || t('system.iam.security.stepUpFailed'))
      cancel()
    })
  })
}

async function complete() {
  const pending = active
  if (!pending?.challenge || !isCurrent(pending) || !/^\d{6}$/.test(code.value) || submitting.value) return
  submitting.value = true
  let session
  try {
    session = await iamAPI.mfa.completeStepUp(pending.challenge.challenge_token, code.value, pending.controller.signal)
  } catch (error) {
    if (isCurrent(pending)) {
      code.value = ''
      submitting.value = false
      ElMessage.error(error.response?.data?.error || t('system.iam.security.invalidCode'))
    }
    return
  }
  if (!isCurrent(pending)) return
  active = null
  clearDialog()
  try {
    authStore.setToken(session.access_token, session.expires_in)
    const context = await authStore.fetchAuthContext()
    pending.resolve(authStore.token === session.access_token && Boolean(context))
  } catch {
    pending.resolve(false)
  }
}

function clearDialog() {
  visible.value = false
  code.value = ''
  submitting.value = false
}

function cancel() {
  const pending = active
  active = null
  clearDialog()
  pending?.controller.abort()
  pending?.resolve(false)
}

watch(visible, open => { if (!open) cancel() }, { flush: 'sync' })
watch([() => authStore.token, () => authStore.authContext], () => {
  if (active && !isCurrent(active)) cancel()
}, { flush: 'sync' })
onBeforeUnmount(cancel)

defineExpose({ request })
</script>
