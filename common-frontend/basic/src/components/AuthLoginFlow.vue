<template>
  <div class="auth-login-container">
    <el-card class="auth-login-card">
      <template #header>
        <div class="auth-login-header">
          <el-icon :size="30"><Platform /></el-icon>
          <h1>{{ title }}</h1>
        </div>
      </template>

      <el-form v-if="step === 'credentials'" :model="credentials" @submit.prevent="submitCredentials">
        <el-form-item :label="t('auth.login.username')">
          <el-input
            v-model="credentials.username"
            :placeholder="t('auth.login.usernamePlaceholder')"
            autocomplete="username"
            size="large"
          >
            <template #prefix><el-icon><User /></el-icon></template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('auth.login.password')">
          <el-input
            v-model="credentials.password"
            type="password"
            :placeholder="t('auth.login.passwordPlaceholder')"
            autocomplete="current-password"
            show-password
            size="large"
          >
            <template #prefix><el-icon><Lock /></el-icon></template>
          </el-input>
        </el-form-item>
        <el-button class="auth-login-primary" type="primary" native-type="submit" :loading="loading" size="large">
          {{ loading ? t('auth.login.loggingIn') : t('auth.login.submit') }}
        </el-button>
      </el-form>

      <el-form v-else-if="step === 'mfa'" :model="mfa" @submit.prevent="submitMFA">
        <div class="auth-login-step-title">
          <el-icon><Key /></el-icon>
          <span>{{ t('auth.login.mfaTitle') }}</span>
        </div>
        <el-form-item :label="t('auth.login.totpCode')">
          <el-input
            v-model="mfa.code"
            :placeholder="t('auth.login.totpPlaceholder')"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            size="large"
          />
        </el-form-item>
        <el-button class="auth-login-primary" type="primary" native-type="submit" :loading="loading" size="large">
          {{ t('auth.login.verify') }}
        </el-button>
        <el-button :icon="ArrowLeft" text @click="restart">{{ t('common.back') }}</el-button>
      </el-form>

      <div v-else class="auth-login-contexts">
        <div class="auth-login-step-title">
          <el-icon><OfficeBuilding /></el-icon>
          <span>{{ t('auth.login.contextTitle') }}</span>
        </div>
        <el-radio-group v-model="selectedContextIndex" class="auth-login-context-list">
          <el-radio
            v-for="(context, index) in selection.contexts"
            :key="contextKey(context)"
            :value="index"
            border
          >
            {{ contextLabel(context) }}
          </el-radio>
        </el-radio-group>
        <el-button
          class="auth-login-primary"
          type="primary"
          :loading="loading"
          :disabled="selectedContextIndex === null"
          size="large"
          @click="submitContext"
        >
          {{ t('auth.login.continue') }}
        </el-button>
        <el-button :icon="ArrowLeft" text @click="restart">{{ t('common.back') }}</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Key, Lock, OfficeBuilding, Platform, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  title: { type: String, required: true },
  login: { type: Function, required: true },
  verifyMfa: { type: Function, required: true },
  selectContext: { type: Function, required: true }
})

const emit = defineEmits(['authenticated'])
const { t } = useI18n()
const step = ref('credentials')
const loading = ref(false)
const credentials = reactive({ username: '', password: '' })
const mfa = reactive({ challengeToken: '', code: '' })
const selection = reactive({ ticket: '', contexts: [] })
const selectedContextIndex = ref(null)

async function submitCredentials() {
  if (!credentials.username.trim() || !credentials.password) {
    ElMessage.warning(t('auth.login.credentialsRequired'))
    return
  }
  await execute(async () => {
    const result = await props.login(credentials.username, credentials.password)
    credentials.password = ''
    handleResult(result)
  })
}

async function submitMFA() {
  if (!/^\d{6}$/.test(mfa.code)) {
    ElMessage.warning(t('auth.login.totpRequired'))
    return
  }
  await execute(async () => {
    const result = await props.verifyMfa(mfa.challengeToken, mfa.code)
    mfa.code = ''
    handleResult(result)
  })
}

async function submitContext() {
  const context = selection.contexts[selectedContextIndex.value]
  if (!context) return
  await execute(async () => {
    const result = await props.selectContext(selection.ticket, context)
    handleResult(result)
  })
}

function handleResult(result) {
  switch (result?.next_action) {
    case 'session_issued':
      ElMessage.success(t('auth.login.success'))
      emit('authenticated')
      return
    case 'verify_mfa':
      mfa.challengeToken = result.mfa.challenge_token
      mfa.code = ''
      step.value = 'mfa'
      return
    case 'select_context':
      selection.ticket = result.selection.selection_ticket
      selection.contexts = result.selection.contexts
      selectedContextIndex.value = result.selection.contexts.length === 1 ? 0 : null
      step.value = 'context'
      return
    default:
      throw new Error('auth_login_next_action_unsupported')
  }
}

function restart() {
  step.value = 'credentials'
  credentials.password = ''
  mfa.challengeToken = ''
  mfa.code = ''
  selection.ticket = ''
  selection.contexts = []
  selectedContextIndex.value = null
}

async function execute(operation) {
  loading.value = true
  try {
    await operation()
  } catch (error) {
    console.error('[AuthLoginFlow] Authentication failed:', error)
    const message = error?.response?.data?.error ||
      (error?.request ? t('auth.login.networkError') : error?.message) ||
      t('auth.login.failed')
    ElMessage.error(message)
  } finally {
    loading.value = false
  }
}

function contextKey(context) {
  return context.type === 'tenant'
    ? `tenant:${context.tenant_membership_id}`
    : 'platform'
}

function contextLabel(context) {
  if (context.type === 'platform') return t('auth.login.platformContext')
  return context.tenant_name || context.tenant_code || context.tenant_id
}
</script>

<style scoped>
.auth-login-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--addp-bg-secondary);
}

.auth-login-card {
  width: min(440px, 100%);
}

.auth-login-header,
.auth-login-step-title {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--addp-text-primary);
}

.auth-login-header {
  justify-content: center;
}

.auth-login-header h1 {
  margin: 0;
  font-size: 22px;
  line-height: 1.3;
  letter-spacing: 0;
}

.auth-login-step-title {
  margin-bottom: 20px;
  font-size: 16px;
  font-weight: 600;
}

.auth-login-primary {
  width: 100%;
}

.auth-login-context-list {
  width: 100%;
  display: grid;
  gap: 10px;
  margin-bottom: 20px;
}

.auth-login-context-list :deep(.el-radio) {
  width: 100%;
  margin: 0;
  overflow: hidden;
}

.auth-login-context-list :deep(.el-radio__label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
