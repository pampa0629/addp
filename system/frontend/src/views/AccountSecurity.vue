<template>
  <div class="account-security-page">
    <header class="account-security-page__header">
      <div>
        <h2>{{ t('system.iam.account.title') }}</h2>
        <p
          :class="{ 'account-security-page__identity--loading': sessionLoading }"
          :role="sessionLoading ? 'status' : undefined"
          :aria-live="sessionLoading ? 'polite' : undefined"
        >
          <el-icon v-if="sessionLoading" class="is-loading"><Loading /></el-icon>
          {{ accountIdentity }}
        </p>
      </div>
      <el-tooltip :content="sessionAssuranceDetail" :disabled="sessionLoading" placement="bottom">
        <el-tag effect="plain">{{ sessionAssuranceLabel }}</el-tag>
      </el-tooltip>
    </header>

    <section class="account-security-page__section">
      <div class="account-security-page__section-heading">
        <h3>{{ t('system.iam.account.security') }}</h3>
        <p>{{ t('system.iam.account.securityDescription') }}</p>
      </div>
      <MFASecurityPanel />
    </section>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Loading } from '@element-plus/icons-vue'
import MFASecurityPanel from '../components/iam/MFASecurityPanel.vue'
import { useAuthStore } from '../store/auth'
import { accountSessionIsLoading, assuranceLevelKey } from '../utils/iamPresentation'

const { t } = useI18n()
const authStore = useAuthStore()
const sessionLoading = computed(() => accountSessionIsLoading(authStore))
const accountIdentity = computed(() => {
  if (sessionLoading.value) return t('system.iam.account.syncing')
  const user = authStore.user
  const name = user?.display_name || user?.local_account?.username || t('system.iam.account.unavailable')
  const username = user?.local_account?.username || ''
  return username && username !== name ? `${name} (${username})` : name
})
const assuranceKey = computed(() => assuranceLevelKey(authStore.authContext?.authentication?.assurance_level))
const assuranceLevel = computed(() => authStore.authContext?.authentication?.assurance_level?.toUpperCase() || '-')
const sessionAssuranceLabel = computed(() => t('system.iam.session.label', {
  state: sessionLoading.value
    ? t('system.iam.session.levels.syncing')
    : t(`system.iam.session.levels.${assuranceKey.value}`)
}))
const sessionAssuranceDetail = computed(() => t('system.iam.session.detail', { level: assuranceLevel.value }))
</script>

<style scoped>
.account-security-page { min-width: 0; color: var(--addp-text-primary); }
.account-security-page__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; margin-bottom: 18px; }
.account-security-page__header h2 { margin: 0 0 6px; font-size: 22px; font-weight: 600; }
.account-security-page__header p, .account-security-page__section-heading p { margin: 0; color: var(--addp-text-secondary); font-size: 13px; }
.account-security-page__identity--loading { display: inline-flex; align-items: center; gap: 6px; }
.account-security-page__section { padding: 18px 20px; border: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); }
.account-security-page__section-heading { padding-bottom: 14px; border-bottom: 1px solid var(--addp-border-color); }
.account-security-page__section-heading h3 { margin: 0 0 6px; font-size: 17px; font-weight: 600; }
@media (max-width: 620px) {
  .account-security-page__header { align-items: stretch; flex-direction: column; }
  .account-security-page__section { padding: 14px; }
}
</style>
