<template>
  <main class="security-policy">
    <header class="page-header">
      <div>
        <h1>{{ t('system.securityPolicy.title') }}</h1>
        <div class="status-line">
          <el-tag :type="form.pendingRestart ? 'warning' : 'success'" effect="plain">
            {{ form.pendingRestart
              ? t('system.securityPolicy.pendingRestart')
              : t('system.securityPolicy.applied') }}
          </el-tag>
          <span>{{ t('system.securityPolicy.versions', { version: form.version, applied: form.appliedVersion }) }}</span>
        </div>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :title="t('system.securityPolicy.reload')" @click="load" />
    </header>

    <el-alert
      v-if="form.pendingRestart"
      class="restart-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('system.securityPolicy.restartNotice')"
    />

    <el-form ref="formRef" v-loading="loading" :model="form" :rules="rules" label-position="top">
      <section class="form-section">
        <h2>{{ t('system.securityPolicy.sessionSection') }}</h2>
        <div class="form-grid">
          <el-form-item :label="t('system.securityPolicy.accessTokenTTL')" prop="accessTokenTTLMinutes">
            <el-input-number v-model="form.accessTokenTTLMinutes" :min="1" :max="60" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.delegatedTokenTTL')" prop="delegatedAccessTokenTTLMinutes">
            <el-input-number v-model="form.delegatedAccessTokenTTLMinutes" :min="1" :max="2" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.resourceTicketTTL')" prop="resourceAccessTicketTTLMinutes">
            <el-input-number v-model="form.resourceAccessTicketTTLMinutes" :min="1" :max="60" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.refreshTokenTTL')" prop="refreshTokenTTLDays">
            <el-input-number v-model="form.refreshTokenTTLDays" :min="1" :max="365" controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <h2>{{ t('system.securityPolicy.oauthSection') }}</h2>
        <div class="form-grid form-grid--three">
          <el-form-item :label="t('system.securityPolicy.authorizationCodeTTL')" prop="oauthAuthorizationCodeTTLMinutes">
            <el-input-number v-model="form.oauthAuthorizationCodeTTLMinutes" :min="1" :max="5" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.deviceCodeTTL')" prop="oauthDeviceCodeTTLMinutes">
            <el-input-number v-model="form.oauthDeviceCodeTTLMinutes" :min="5" :max="30" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.devicePollInterval')" prop="oauthDevicePollIntervalSeconds">
            <el-input-number v-model="form.oauthDevicePollIntervalSeconds" :min="5" :max="60" controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <h2>{{ t('system.securityPolicy.invitationSection') }}</h2>
        <div class="form-grid">
          <el-form-item :label="t('system.securityPolicy.invitationTTL')" prop="tenantInvitationTTLHours">
            <el-input-number v-model="form.tenantInvitationTTLHours" :min="1" :max="720" controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <h2>{{ t('system.securityPolicy.rateLimitSection') }}</h2>
        <div class="form-grid">
          <el-form-item :label="t('system.securityPolicy.publicRateLimit')" prop="oauthPublicRateLimitPerMinute">
            <el-input-number v-model="form.oauthPublicRateLimitPerMinute" :min="1" :max="10000" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('system.securityPolicy.userRateLimit')" prop="oauthUserRateLimitPerMinute">
            <el-input-number v-model="form.oauthUserRateLimitPerMinute" :min="1" :max="10000" controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <footer v-if="canUpdate" class="form-actions">
        <el-button type="primary" :icon="Check" :loading="saving" @click="save">
          {{ t('system.securityPolicy.save') }}
        </el-button>
      </footer>
    </el-form>
  </main>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getIAMSecurityPolicy, updateIAMSecurityPolicy } from '../api/securityPolicy'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const saving = ref(false)
const canUpdate = computed(() => authStore.hasPermission('iam.security_policy.update'))
const form = reactive({
  version: 0,
  appliedVersion: 0,
  pendingRestart: false,
  accessTokenTTLMinutes: 15,
  delegatedAccessTokenTTLMinutes: 2,
  resourceAccessTicketTTLMinutes: 15,
  refreshTokenTTLDays: 30,
  oauthAuthorizationCodeTTLMinutes: 5,
  oauthDeviceCodeTTLMinutes: 10,
  oauthDevicePollIntervalSeconds: 5,
  tenantInvitationTTLHours: 168,
  oauthPublicRateLimitPerMinute: 60,
  oauthUserRateLimitPerMinute: 30
})

const rules = {
  resourceAccessTicketTTLMinutes: [{
    validator: (_rule, value, callback) => value <= form.accessTokenTTLMinutes
      ? callback()
      : callback(new Error(t('system.securityPolicy.resourceTicketTooLong'))),
    trigger: 'change'
  }]
}

function applyValue(value) {
  form.version = value.version
  form.appliedVersion = value.applied_version
  form.pendingRestart = value.pending_restart
  form.accessTokenTTLMinutes = value.access_token_ttl_minutes
  form.delegatedAccessTokenTTLMinutes = value.delegated_access_token_ttl_minutes
  form.resourceAccessTicketTTLMinutes = value.resource_access_ticket_ttl_minutes
  form.refreshTokenTTLDays = value.refresh_token_ttl_days
  form.oauthAuthorizationCodeTTLMinutes = value.oauth_authorization_code_ttl_minutes
  form.oauthDeviceCodeTTLMinutes = value.oauth_device_code_ttl_minutes
  form.oauthDevicePollIntervalSeconds = value.oauth_device_poll_interval_seconds
  form.tenantInvitationTTLHours = value.tenant_invitation_ttl_hours
  form.oauthPublicRateLimitPerMinute = value.oauth_public_rate_limit_per_minute
  form.oauthUserRateLimitPerMinute = value.oauth_user_rate_limit_per_minute
}

async function load() {
  loading.value = true
  try {
    applyValue(await getIAMSecurityPolicy())
  } catch (_error) {
    ElMessage.error(t('system.securityPolicy.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    const value = await updateIAMSecurityPolicy({
      version: form.version,
      access_token_ttl_minutes: form.accessTokenTTLMinutes,
      delegated_access_token_ttl_minutes: form.delegatedAccessTokenTTLMinutes,
      resource_access_ticket_ttl_minutes: form.resourceAccessTicketTTLMinutes,
      refresh_token_ttl_days: form.refreshTokenTTLDays,
      oauth_authorization_code_ttl_minutes: form.oauthAuthorizationCodeTTLMinutes,
      oauth_device_code_ttl_minutes: form.oauthDeviceCodeTTLMinutes,
      oauth_device_poll_interval_seconds: form.oauthDevicePollIntervalSeconds,
      tenant_invitation_ttl_hours: form.tenantInvitationTTLHours,
      oauth_public_rate_limit_per_minute: form.oauthPublicRateLimitPerMinute,
      oauth_user_rate_limit_per_minute: form.oauthUserRateLimitPerMinute
    })
    applyValue(value)
    ElMessage.success(t('system.securityPolicy.saveSuccess'))
  } catch (error) {
    if (error?.response?.status === 409) {
      ElMessage.warning(t('system.securityPolicy.versionConflict'))
      await load()
    } else {
      ElMessage.error(t('system.securityPolicy.saveFailed'))
    }
  } finally {
    saving.value = false
  }
}

load()
</script>

<style scoped>
.security-policy {
  width: min(1040px, 100%);
  margin: 0 auto;
  padding: 28px 32px 40px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 24px;
}

.page-header h1 {
  margin: 0 0 10px;
  color: var(--addp-text-primary);
  font-size: 24px;
  font-weight: 600;
  letter-spacing: 0;
}

.status-line {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.restart-alert { margin-bottom: 24px; }

.form-section {
  padding: 0 0 22px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--addp-border-color);
}

.form-section h2 {
  margin: 0 0 18px;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 24px;
}

.form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.form-grid :deep(.el-input-number) { width: 100%; }
.form-actions { display: flex; justify-content: flex-end; }

@media (max-width: 760px) {
  .security-policy { padding: 20px 16px 32px; }
  .form-grid, .form-grid--three { grid-template-columns: 1fr; }
  .status-line { align-items: flex-start; flex-direction: column; }
}
</style>
