<template>
  <div class="iam-workbench">
    <header class="iam-page-header">
      <div>
        <h2>{{ t('system.iam.title') }}</h2>
        <div class="iam-context-line">
          <el-tag effect="plain">{{ contextLabel }}</el-tag>
          <span>{{ t('system.iam.assurance', { level: authContext?.authentication?.assurance_level?.toUpperCase() || '-' }) }}</span>
        </div>
      </div>
      <el-button :icon="Refresh" :loading="refreshing" circle :aria-label="t('system.iam.common.refresh')" @click="refreshContext" />
    </header>

    <el-alert v-if="!availableTabs.length" type="warning" :closable="false" show-icon :title="t('system.iam.noPermission')" />
    <el-tabs v-else v-model="activeTab" class="iam-tabs">
      <el-tab-pane v-for="tab in availableTabs" :key="tab.key" :name="tab.key">
        <template #label><el-icon><component :is="tab.icon" /></el-icon><span>{{ t(tab.label) }}</span></template>
        <component :is="tab.component" v-if="activeTab === tab.key" v-bind="tab.props || {}" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed, markRaw, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Bell, DocumentChecked, OfficeBuilding, Refresh, Tickets, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import AuditPanel from '../components/iam/AuditPanel.vue'
import IdentityChangesPanel from '../components/iam/IdentityChangesPanel.vue'
import PlatformTenantsPanel from '../components/iam/PlatformTenantsPanel.vue'
import PlatformUsersPanel from '../components/iam/PlatformUsersPanel.vue'
import TenantInvitationsPanel from '../components/iam/TenantInvitationsPanel.vue'
import TenantMembershipsPanel from '../components/iam/TenantMembershipsPanel.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const activeTab = ref('')
const refreshing = ref(false)
const authContext = computed(() => authStore.authContext)
const contextType = computed(() => authStore.contextType)
const contextLabel = computed(() => contextType.value === 'platform'
  ? t('system.iam.context.platform')
  : t('system.iam.context.tenant', { id: authContext.value?.context?.tenant_id || '-' }))

const allTabs = [
  { key: 'tenants', context: 'platform', permission: 'platform.tenant.read', label: 'system.iam.tabs.tenants', icon: markRaw(OfficeBuilding), component: markRaw(PlatformTenantsPanel) },
  { key: 'users', context: 'platform', permission: 'iam.user.read', label: 'system.iam.tabs.users', icon: markRaw(User), component: markRaw(PlatformUsersPanel) },
  { key: 'identity-changes', context: 'platform', permission: 'iam.platform_identity_change.read', label: 'system.iam.tabs.identityChanges', icon: markRaw(DocumentChecked), component: markRaw(IdentityChangesPanel) },
  { key: 'platform-audit', context: 'platform', permission: 'audit.event.read', label: 'system.iam.tabs.platformAudit', icon: markRaw(Bell), component: markRaw(AuditPanel), props: { scope: 'platform' } },
  { key: 'memberships', context: 'tenant', permission: 'iam.tenant_membership.read', label: 'system.iam.tabs.memberships', icon: markRaw(User), component: markRaw(TenantMembershipsPanel) },
  { key: 'invitations', context: 'tenant', permission: 'iam.tenant_invitation.read', label: 'system.iam.tabs.invitations', icon: markRaw(Tickets), component: markRaw(TenantInvitationsPanel) },
  { key: 'tenant-audit', context: 'tenant', permission: 'audit.tenant_event.read', label: 'system.iam.tabs.tenantAudit', icon: markRaw(Bell), component: markRaw(AuditPanel), props: { scope: 'tenant' } }
]
const availableTabs = computed(() => allTabs.filter((tab) => tab.context === contextType.value && authStore.hasPermission(tab.permission)))

watch(availableTabs, (tabs) => {
  if (!tabs.some((tab) => tab.key === activeTab.value)) activeTab.value = tabs[0]?.key || ''
}, { immediate: true })

async function refreshContext() {
  refreshing.value = true
  try {
    await authStore.fetchAuthContext()
    ElMessage.success(t('system.iam.common.refreshed'))
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    refreshing.value = false
  }
}
</script>

<style>
.iam-workbench { min-width: 0; color: var(--addp-text-primary); }
.iam-page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; margin-bottom: 12px; }
.iam-page-header h2 { margin: 0 0 8px; font-size: 22px; font-weight: 600; letter-spacing: 0; }
.iam-context-line { display: flex; align-items: center; gap: 10px; color: var(--addp-text-secondary); font-size: 13px; }
.iam-tabs > .el-tabs__content { overflow: visible; }
.iam-tabs .el-tabs__item { display: inline-flex; align-items: center; gap: 6px; }
.iam-panel { min-width: 0; padding-top: 6px; }
.iam-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.iam-filters { display: flex; align-items: center; gap: 10px; min-width: 0; }
.iam-filters > .el-input { width: 220px; }
.iam-filters > .el-select { width: 150px; }
.iam-pagination { display: flex; justify-content: flex-end; margin-top: 18px; }
.iam-primary-cell { display: flex; flex-direction: column; gap: 3px; }
.iam-primary-cell strong { font-weight: 600; color: var(--addp-text-primary); }
.iam-primary-cell span { color: var(--addp-text-secondary); font-size: 12px; }
.iam-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }
.iam-form-span { grid-column: 1 / -1; }
@media (max-width: 760px) {
  .iam-toolbar, .iam-filters { align-items: stretch; flex-direction: column; }
  .iam-filters > .el-input, .iam-filters > .el-select { width: 100%; }
  .iam-form-grid { grid-template-columns: 1fr; }
  .iam-form-span { grid-column: auto; }
}
</style>
