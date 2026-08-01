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

    <section v-if="showTenantRoleSetup && activeTab !== 'role-assignments'" class="iam-setup-guide" aria-live="polite">
      <el-icon class="iam-setup-guide__icon"><InfoFilled /></el-icon>
      <div class="iam-setup-guide__content">
        <strong>{{ t('system.iam.setup.title') }}</strong>
        <span>{{ t('system.iam.setup.description') }}</span>
      </div>
      <el-button type="primary" plain :icon="UserFilled" @click="selectTab('role-assignments')">
        {{ t('system.iam.setup.action') }}
      </el-button>
    </section>

    <el-alert v-if="!availableTabs.length" type="warning" :closable="false" show-icon :title="t('system.iam.noPermission')" />
    <el-tabs v-else v-model="activeTab" class="iam-tabs" @tab-change="selectTab">
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
import { Bell, DocumentChecked, InfoFilled, Lock, OfficeBuilding, Refresh, Tickets, User, UserFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import AuditPanel from '../components/iam/AuditPanel.vue'
import IdentityChangesPanel from '../components/iam/IdentityChangesPanel.vue'
import PlatformTenantsPanel from '../components/iam/PlatformTenantsPanel.vue'
import PlatformUsersPanel from '../components/iam/PlatformUsersPanel.vue'
import TenantInvitationsPanel from '../components/iam/TenantInvitationsPanel.vue'
import TenantMembershipsPanel from '../components/iam/TenantMembershipsPanel.vue'
import TenantRolesPanel from '../components/iam/TenantRolesPanel.vue'
import TenantRoleAssignmentsPanel from '../components/iam/TenantRoleAssignmentsPanel.vue'
import MFASecurityPanel from '../components/iam/MFASecurityPanel.vue'
import { needsTenantRoleSetup } from '../utils/iamRoles'
import { navigateSystemRoute } from '../utils/moduleNavigation'
import { resolveIAMRouteState } from '../utils/routeState'

const { t } = useI18n()
const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()
const activeTab = ref('')
const refreshing = ref(false)
const authContext = computed(() => authStore.authContext)
const contextType = computed(() => authStore.contextType)
const contextLabel = computed(() => contextType.value === 'platform'
  ? t('system.iam.context.platform')
  : t('system.iam.context.tenant', { id: authContext.value?.context?.tenant_id || '-' }))
const showTenantRoleSetup = computed(() => needsTenantRoleSetup(authContext.value))

const allTabs = [
  { key: 'security', context: 'any', permission: null, label: 'system.iam.tabs.security', icon: markRaw(Lock), component: markRaw(MFASecurityPanel) },
  { key: 'tenants', context: 'platform', permission: 'platform.tenant.read', label: 'system.iam.tabs.tenants', icon: markRaw(OfficeBuilding), component: markRaw(PlatformTenantsPanel) },
  { key: 'users', context: 'platform', permission: 'iam.user.read', label: 'system.iam.tabs.users', icon: markRaw(User), component: markRaw(PlatformUsersPanel) },
  { key: 'identity-changes', context: 'platform', permission: 'iam.platform_identity_change.read', label: 'system.iam.tabs.identityChanges', icon: markRaw(DocumentChecked), component: markRaw(IdentityChangesPanel) },
  { key: 'platform-audit', context: 'platform', permission: 'audit.event.read', label: 'system.iam.tabs.platformAudit', icon: markRaw(Bell), component: markRaw(AuditPanel), props: { scope: 'platform' } },
  { key: 'memberships', context: 'tenant', permission: 'iam.tenant_membership.read', label: 'system.iam.tabs.memberships', icon: markRaw(User), component: markRaw(TenantMembershipsPanel) },
  { key: 'invitations', context: 'tenant', permission: 'iam.tenant_invitation.read', label: 'system.iam.tabs.invitations', icon: markRaw(Tickets), component: markRaw(TenantInvitationsPanel) },
  { key: 'roles', context: 'tenant', permission: 'iam.tenant_role.read', label: 'system.iam.tabs.roles', icon: markRaw(DocumentChecked), component: markRaw(TenantRolesPanel) },
  { key: 'role-assignments', context: 'tenant', permission: 'iam.tenant_role_assignment.read', label: 'system.iam.tabs.roleAssignments', icon: markRaw(UserFilled), component: markRaw(TenantRoleAssignmentsPanel) },
  { key: 'tenant-audit', context: 'tenant', permission: 'audit.tenant_event.read', label: 'system.iam.tabs.tenantAudit', icon: markRaw(Bell), component: markRaw(AuditPanel), props: { scope: 'tenant' } }
]
const availableTabs = computed(() => allTabs.filter((tab) =>
  (tab.context === 'any' || tab.context === contextType.value) && (!tab.permission || authStore.hasPermission(tab.permission))))

async function restoreTabFromRoute() {
  const tabs = availableTabs.value
  const routeState = resolveIAMRouteState(tabs.map(tab => tab.key), route.query)
  activeTab.value = routeState.activeTab

  if (routeState.changed) {
    await navigateSystemRoute(router, { name: 'IAMWorkbench', query: routeState.query }, { history: 'replace' })
  }
}

async function selectTab(tab) {
  const tabKey = String(tab || '')
  const defaultTab = availableTabs.value[0]?.key || ''
  if (!availableTabs.value.some(item => item.key === tabKey)) return
  activeTab.value = tabKey
  await navigateSystemRoute(router, {
    name: 'IAMWorkbench',
    query: tabKey === defaultTab ? {} : { tab: tabKey }
  }, { history: 'replace' })
}

watch([availableTabs, () => route.query], restoreTabFromRoute, { immediate: true })

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
.iam-setup-guide { display: flex; align-items: center; gap: 12px; min-width: 0; padding: 12px 14px; margin-bottom: 14px; border: 1px solid var(--addp-border-color); border-left: 3px solid var(--el-color-primary); background: var(--addp-bg-primary); }
.iam-setup-guide__icon { flex: 0 0 auto; color: var(--el-color-primary); font-size: 20px; }
.iam-setup-guide__content { display: flex; flex: 1; flex-direction: column; gap: 3px; min-width: 0; }
.iam-setup-guide__content strong { color: var(--addp-text-primary); font-size: 14px; font-weight: 600; }
.iam-setup-guide__content span { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
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
  .iam-setup-guide { align-items: stretch; flex-wrap: wrap; }
  .iam-setup-guide__content { flex-basis: calc(100% - 36px); }
  .iam-setup-guide > .el-button { width: 100%; margin-left: 0; }
  .iam-toolbar, .iam-filters { align-items: stretch; flex-direction: column; }
  .iam-filters > .el-input, .iam-filters > .el-select { width: 100%; }
  .iam-form-grid { grid-template-columns: 1fr; }
  .iam-form-span { grid-column: auto; }
}
</style>
