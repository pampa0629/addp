<template>
  <section class="iam-panel">
    <div class="iam-security-row">
      <div class="iam-security-row__identity">
        <el-icon><Iphone /></el-icon>
        <div>
          <strong>{{ t('system.iam.security.authenticator') }}</strong>
          <span>{{ t('system.iam.security.totp') }}</span>
        </div>
      </div>
      <div class="iam-security-row__action">
        <el-tag :type="status?.totp_enrolled ? 'success' : 'info'">
          {{ t(status?.totp_enrolled ? 'system.iam.security.enabled' : 'system.iam.security.notEnabled') }}
        </el-tag>
        <el-button v-if="status && !status.totp_enrolled" type="primary" :icon="Lock" @click="openEnrollment">
          {{ t('system.iam.security.enable') }}
        </el-button>
      </div>
    </div>

    <el-dialog v-model="visible" :title="t('system.iam.security.enableAuthenticator')" width="min(520px, calc(100% - 24px))" @closed="reset">
      <el-form v-if="phase === 'password'" label-position="top" @submit.prevent="beginEnrollment">
        <el-form-item :label="t('system.iam.security.currentPassword')">
          <el-input v-model="currentPassword" type="password" show-password autocomplete="current-password" @keyup.enter="beginEnrollment" />
        </el-form-item>
      </el-form>
      <div v-else class="iam-enrollment">
        <canvas ref="qrCanvas" class="iam-enrollment__qr" :aria-label="t('system.iam.security.qrCode')" />
        <div class="iam-enrollment__secret">
          <span>{{ t('system.iam.security.manualKey') }}</span>
          <code>{{ enrollment.secret }}</code>
        </div>
        <el-form label-position="top" @submit.prevent="completeEnrollment">
          <el-form-item :label="t('system.iam.security.verificationCode')">
            <el-input v-model="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" @keyup.enter="completeEnrollment" />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="visible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button v-if="phase === 'password'" type="primary" :loading="submitting" :disabled="!currentPassword" @click="beginEnrollment">
          {{ t('system.iam.common.continue') }}
        </el-button>
        <el-button v-else type="primary" :loading="submitting" :disabled="code.length !== 6" @click="completeEnrollment">
          {{ t('system.iam.security.verifyAndEnable') }}
        </el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { nextTick, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Iphone, Lock } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const status = ref(null)
const visible = ref(false)
const phase = ref('password')
const currentPassword = ref('')
const code = ref('')
const enrollment = ref(null)
const submitting = ref(false)
const qrCanvas = ref()

async function loadStatus() {
  try {
    status.value = await iamAPI.mfa.status()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  }
}

function openEnrollment() {
  reset()
  visible.value = true
}

function reset() {
  phase.value = 'password'
  currentPassword.value = ''
  code.value = ''
  enrollment.value = null
}

async function beginEnrollment() {
  if (!currentPassword.value || submitting.value) return
  submitting.value = true
  try {
    enrollment.value = await iamAPI.mfa.beginEnrollment(currentPassword.value)
    currentPassword.value = ''
    phase.value = 'verify'
    await nextTick()
    await QRCode.toCanvas(qrCanvas.value, enrollment.value.otpauth_uri, { width: 208, margin: 1 })
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.security.enrollmentFailed'))
  } finally {
    submitting.value = false
  }
}

async function completeEnrollment() {
  if (!enrollment.value || code.value.length !== 6 || submitting.value) return
  submitting.value = true
  try {
    const session = await iamAPI.mfa.completeEnrollment(enrollment.value.enrollment_token, code.value)
    authStore.setToken(session.access_token, session.expires_in)
    await authStore.fetchAuthContext()
    status.value = { totp_enrolled: true }
    visible.value = false
    ElMessage.success(t('system.iam.security.enabledSuccess'))
  } catch (error) {
    code.value = ''
    ElMessage.error(error.response?.data?.error || t('system.iam.security.invalidCode'))
  } finally {
    submitting.value = false
  }
}

onMounted(loadStatus)
</script>

<style scoped>
.iam-security-row { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 16px 0; border-bottom: 1px solid var(--addp-border-color); }
.iam-security-row__identity, .iam-security-row__action { display: flex; align-items: center; gap: 12px; min-width: 0; }
.iam-security-row__identity > .el-icon { flex: 0 0 auto; font-size: 24px; color: var(--el-color-primary); }
.iam-security-row__identity > div { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.iam-security-row__identity strong { font-size: 14px; font-weight: 600; color: var(--addp-text-primary); }
.iam-security-row__identity span { font-size: 13px; color: var(--addp-text-secondary); }
.iam-enrollment { display: flex; flex-direction: column; align-items: center; gap: 18px; }
.iam-enrollment__qr { width: 208px; height: 208px; border: 1px solid var(--addp-border-color); }
.iam-enrollment__secret { display: flex; flex-direction: column; align-items: center; gap: 6px; width: 100%; color: var(--addp-text-secondary); font-size: 13px; }
.iam-enrollment__secret code { max-width: 100%; overflow-wrap: anywhere; color: var(--addp-text-primary); font-size: 14px; }
.iam-enrollment .el-form { width: 100%; }
@media (max-width: 620px) {
  .iam-security-row { align-items: stretch; flex-direction: column; }
  .iam-security-row__action { justify-content: space-between; }
}
</style>
