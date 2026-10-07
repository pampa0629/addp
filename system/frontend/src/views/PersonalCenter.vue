<template>
  <div class="personal-center">
    <header class="personal-center__header">
      <div>
        <h2>{{ t('system.iam.account.title') }}</h2>
        <p role="status">{{ accountIdentity }}</p>
        <el-button v-if="organization && !sessionLoading && authStore.contextType === 'tenant'" class="personal-center__summary" link @click="activeTab = 'organization'">
          <span v-if="primaryDepartment">{{ primaryDepartment.name }} · {{ t('system.iam.organization.primary') }} · {{ relationRole(primaryDepartment.relation_role) }}</span>
          <span>{{ t('system.iam.account.groupCount', { count: organization.project_groups.length }) }}</span>
        </el-button>
      </div>
      <el-tooltip :content="sessionAssuranceDetail" :disabled="sessionLoading" placement="bottom">
        <el-tag effect="plain">{{ sessionAssuranceLabel }}</el-tag>
      </el-tooltip>
    </header>

    <el-alert v-if="organizationError" class="personal-center__error" :title="t('system.iam.account.organizationFailed')" type="error" :closable="false" show-icon>
      <el-button link type="primary" @click="loadOrganization">{{ t('system.iam.account.retry') }}</el-button>
    </el-alert>
    <el-alert v-if="invalidTab" :title="t('system.iam.account.invalidTab')" type="error" :closable="false">
      <el-button link type="primary" @click="activeTab = 'basic'">{{ t('system.iam.account.basic') }}</el-button>
    </el-alert>
    <el-tabs v-else v-model="activeTab">
      <el-tab-pane :label="t('system.iam.account.basic')" name="basic">
        <section class="personal-center__section">
          <el-skeleton v-if="sessionLoading" :rows="4" animated />
          <dl v-else class="personal-center__facts">
            <dt>{{ t('system.iam.account.displayName') }}</dt><dd>{{ authStore.user?.display_name || t('system.iam.account.notSet') }}</dd>
            <dt>{{ t('system.iam.account.username') }}</dt><dd>{{ authStore.user?.local_account?.username || t('system.iam.account.externalAccount') }}</dd>
            <dt>{{ t('system.iam.account.email') }}</dt><dd>{{ authStore.user?.primary_email || t('system.iam.account.notSet') }}</dd>
            <dt>{{ t('system.iam.account.tenant') }}</dt>
            <dd>{{ organization?.tenant?.name || (authStore.contextType === 'platform' ? t('system.iam.account.platformContext') : t(organizationLoading ? 'system.iam.account.syncing' : 'system.iam.account.unavailable')) }}</dd>
          </dl>
        </section>
      </el-tab-pane>
      <el-tab-pane :label="t('system.iam.account.organization')" name="organization">
        <section class="personal-center__section">
          <el-skeleton v-if="organizationLoading || sessionLoading" :rows="5" animated />
          <template v-else-if="organization">
            <el-empty v-if="!organization.tenant" :description="t('system.iam.account.platformContext')" />
            <template v-else>
              <h3>{{ t('system.iam.account.departments') }}</h3>
              <el-empty v-if="!organization.departments.length" :description="t('system.iam.account.noDepartments')" :image-size="64" />
              <div v-for="department in organization.departments" :key="department.id" class="personal-center__membership">
                <div class="personal-center__organization-name">
                  <strong>{{ department.name }}</strong>
                  <span>{{ department.path.map(item => item.name).join(' / ') }}</span>
                  <code>{{ department.code }}</code>
                </div>
                <div class="personal-center__tags">
                  <el-tag :type="department.membership_type === 'primary' ? 'primary' : 'info'">{{ t(`system.iam.organization.${department.membership_type}`) }}</el-tag>
                  <el-tag effect="plain">{{ relationRole(department.relation_role) }}</el-tag>
                </div>
              </div>
              <h3>{{ t('system.iam.account.projectGroups') }}</h3>
              <el-empty v-if="!organization.project_groups.length" :description="t('system.iam.account.noProjectGroups')" :image-size="64" />
              <div v-for="group in organization.project_groups" :key="group.id" class="personal-center__membership">
                <div class="personal-center__organization-name"><strong>{{ group.name }}</strong><code>{{ group.code }}</code></div>
                <el-tag effect="plain">{{ relationRole(group.relation_role) }}</el-tag>
              </div>
            </template>
          </template>
        </section>
      </el-tab-pane>
      <el-tab-pane :label="t('system.iam.account.security')" name="security" lazy>
        <section v-if="activeTab === 'security' && !sessionLoading" :key="requestIdentity" class="personal-center__section">
          <h3>{{ t('system.iam.account.security') }}</h3>
          <p>{{ t('system.iam.account.securityDescription') }}</p>
          <PasswordSecurityPanel v-if="authStore.user?.local_account" />
          <MFASecurityPanel />
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import MFASecurityPanel from '../components/iam/MFASecurityPanel.vue'
import PasswordSecurityPanel from '../components/iam/PasswordSecurityPanel.vue'
import { useAuthStore } from '../store/auth'
import { iamAPI } from '../api/iam'
import { accountSessionIsLoading, assuranceLevelKey } from '../utils/iamPresentation'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const sessionLoading = computed(() => accountSessionIsLoading(authStore))
const invalidTab = computed(() => route.query.tab !== undefined && !['organization', 'security'].includes(route.query.tab))
const activeTab = computed({
  get: () => route.query.tab || 'basic',
  set: tab => router.push({ path: '/account', query: tab === 'basic' ? {} : { tab } })
})
const organization = ref(null)
const organizationLoading = ref(false)
const organizationError = ref(false)
let requestSequence = 0
const requestIdentity = computed(() => {
  const context = authStore.authContext
  if (sessionLoading.value || !authStore.isAuthenticated || authStore.sessionStatus === 'error' || !context) return null
  return JSON.stringify([context.principal.id, context.context, context.authorization?.authorization_version])
})
const primaryDepartment = computed(() => organization.value?.departments.find(item => item.membership_type === 'primary'))
const relationRole = role => t(`system.iam.organization.${role}`)
const accountIdentity = computed(() => {
  if (sessionLoading.value) return t('system.iam.account.syncing')
  const user = authStore.user
  const name = user?.display_name || user?.local_account?.username || t('system.iam.account.unavailable')
  const username = user?.local_account?.username || ''
  return username && username !== name ? `${name} (${username})` : name
})
const assuranceKey = computed(() => assuranceLevelKey(authStore.authContext?.authentication?.assurance_level))
const sessionAssuranceLabel = computed(() => t('system.iam.session.label', {
  state: t(sessionLoading.value ? 'system.iam.session.levels.syncing' : `system.iam.session.levels.${assuranceKey.value}`)
}))
const sessionAssuranceDetail = computed(() => t('system.iam.session.detail', { level: authStore.authContext?.authentication?.assurance_level?.toUpperCase() || '-' }))

async function loadOrganization() {
  const sequence = ++requestSequence
  const identity = requestIdentity.value
  organization.value = null
  organizationError.value = false
  organizationLoading.value = Boolean(identity)
  if (!identity) return
  try {
    const result = await iamAPI.self.organization()
    if (!result || !Array.isArray(result.departments) || !Array.isArray(result.project_groups) ||
      (result.tenant !== null && !result.tenant?.name)) throw new Error('Invalid organization response')
    if (sequence === requestSequence && identity === requestIdentity.value) organization.value = result
  } catch {
    if (sequence === requestSequence && identity === requestIdentity.value) organizationError.value = true
  } finally {
    if (sequence === requestSequence) organizationLoading.value = false
  }
}
watch(requestIdentity, loadOrganization, { immediate: true })
watch(() => route.query.tab, tab => {
  if (tab === 'basic') router.replace({ path: '/account', query: {} })
}, { immediate: true })
onBeforeUnmount(() => { requestSequence++ })
</script>

<style scoped>
.personal-center { min-width: 0; color: var(--addp-text-primary); }
.personal-center__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; margin-bottom: 18px; }
.personal-center h2 { margin: 0 0 6px; font-size: 22px; font-weight: 600; }
.personal-center p { margin: 0; color: var(--addp-text-secondary); font-size: 13px; overflow-wrap: anywhere; }
.personal-center__summary { margin-top: 10px; height: auto; white-space: normal; text-align: left; }
.personal-center__summary :deep(span) { display: flex; flex-wrap: wrap; gap: 6px 12px; }
.personal-center__error { margin-bottom: 14px; }
.personal-center__section { padding: 18px 20px; border: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); }
.personal-center h3 { margin: 0 0 12px; font-size: 17px; font-weight: 600; }
.personal-center__facts { display: grid; grid-template-columns: minmax(90px, 140px) minmax(0, 1fr); gap: 18px; margin: 0; }
.personal-center__facts dt { color: var(--addp-text-secondary); }
.personal-center__facts dd { margin: 0; overflow-wrap: anywhere; }
.personal-center__membership { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 0; border-bottom: 1px solid var(--addp-border-color); }
.personal-center__membership + h3 { margin-top: 24px; }
.personal-center__organization-name { display: flex; flex-direction: column; gap: 6px; min-width: 0; overflow-wrap: anywhere; }
.personal-center__organization-name span, .personal-center__organization-name code { font-size: 13px; color: var(--addp-text-secondary); }
.personal-center__tags { display: flex; flex-wrap: wrap; gap: 8px; }
@media (max-width: 620px) {
  .personal-center__header, .personal-center__membership { align-items: flex-start; flex-direction: column; }
  .personal-center__section { padding: 14px; }
  .personal-center__facts { grid-template-columns: 90px minmax(0, 1fr); }
}
</style>
