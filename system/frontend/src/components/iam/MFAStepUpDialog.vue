<template>
  <el-dialog v-model="visible" :title="t('system.iam.security.stepUpTitle')" width="min(440px, calc(100% - 24px))" :close-on-click-modal="false" @closed="cancel">
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
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const visible = ref(false)
const submitting = ref(false)
const code = ref('')
const challenge = ref(null)
let resolveRequest = null

async function request() {
  if (resolveRequest) return false
  try {
    challenge.value = await iamAPI.mfa.beginStepUp()
    code.value = ''
    visible.value = true
    return await new Promise((resolve) => { resolveRequest = resolve })
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.security.stepUpFailed'))
    return false
  }
}

async function complete() {
  if (!challenge.value || code.value.length !== 6 || submitting.value) return
  submitting.value = true
  try {
    const session = await iamAPI.mfa.completeStepUp(challenge.value.challenge_token, code.value)
    authStore.setToken(session.access_token, session.expires_in)
    await authStore.fetchAuthContext()
    const resolve = resolveRequest
    resolveRequest = null
    visible.value = false
    resolve?.(true)
  } catch (error) {
    code.value = ''
    ElMessage.error(error.response?.data?.error || t('system.iam.security.invalidCode'))
  } finally {
    submitting.value = false
  }
}

function cancel() {
  challenge.value = null
  code.value = ''
  const resolve = resolveRequest
  resolveRequest = null
  resolve?.(false)
}

defineExpose({ request })
</script>
