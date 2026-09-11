<template>
  <div class="iam-category-page">
    <header class="iam-page-header">
      <h2>{{ pageTitle }}</h2>
    </header>

    <section v-if="showTenantRoleSetup && activeTab !== 'role-assignments'" class="iam-setup-guide" aria-live="polite">
      <el-icon class="iam-setup-guide__icon"><InfoFilled /></el-icon>
      <div class="iam-setup-guide__content">
        <strong>{{ t('system.iam.setup.title') }}</strong>
        <span>{{ t('system.iam.setup.description') }}</span>
      </div>
      <el-button type="primary" plain :icon="UserFilled" @click="openRoleAssignments">
        {{ t('system.iam.setup.action') }}
      </el-button>
    </section>

    <el-alert v-if="!availableTabs.length" type="warning" :closable="false" show-icon :title="t('system.iam.noPermission')" />
    <el-tabs v-else v-model="activeTab" class="iam-tabs" @tab-change="selectTab">
      <el-tab-pane v-for="tab in availableTabs" :key="`${tab.key}:${tab.context}`" :name="tab.key">
        <template #label>
          <el-icon><component :is="tabIcon(tab.panel)" /></el-icon>
          <span>{{ t(tab.label) }}</span>
        </template>
        <component
          :is="panelComponents[tab.panel]"
          v-if="activeTab === tab.key"
          v-bind="tab.props || {}"
        />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed, markRaw, ref, watch } from 'vue'
import {
  Bell,
  Connection,
  DocumentChecked,
  InfoFilled,
  Key,
  OfficeBuilding,
  Setting,
  Share,
  Tickets,
  User,
  UserFilled
} from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { availableIAMTabs, findIAMPage } from '../config/iamNavigation'
import AuditPanel from '../components/iam/AuditPanel.vue'
import APIConsumersPanel from '@/components/iam/APIConsumersPanel.vue'
import DepartmentsPanel from '../components/iam/DepartmentsPanel.vue'
import IdentityChangesPanel from '../components/iam/IdentityChangesPanel.vue'
import OAuthClientsPanel from '../components/iam/OAuthClientsPanel.vue'
import PlatformTenantsPanel from '../components/iam/PlatformTenantsPanel.vue'
import PlatformUsersPanel from '../components/iam/PlatformUsersPanel.vue'
import ProjectGroupsPanel from '../components/iam/ProjectGroupsPanel.vue'
import TenantInvitationsPanel from '../components/iam/TenantInvitationsPanel.vue'
import ServicePrincipalAccountsPanel from '../components/iam/ServicePrincipalAccountsPanel.vue'
import TenantUserAccountsPanel from '../components/iam/TenantUserAccountsPanel.vue'
import TenantRoleAssignmentsPanel from '../components/iam/TenantRoleAssignmentsPanel.vue'
import TenantRolesPanel from '../components/iam/TenantRolesPanel.vue'
import SecurityPolicy from './SecurityPolicy.vue'
import { needsTenantRoleSetup } from '../utils/iamRoles'
import { navigateSystemRoute } from '../utils/moduleNavigation'
import { resolveIAMCategoryRouteState } from '../utils/routeState'

const { t } = useI18n()
const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()
const activeTab = ref('')
const authContext = computed(() => authStore.authContext)
const contextType = computed(() => authStore.contextType)
const pageKey = computed(() => String(route.meta?.iamPage || ''))
const pageDefinition = computed(() => findIAMPage(pageKey.value))
const pageTitle = computed(() => pageDefinition.value ? t(pageDefinition.value.label) : t('system.iam.title'))
const showTenantRoleSetup = computed(() => needsTenantRoleSetup(authContext.value))
const availableTabs = computed(() => availableIAMTabs(pageKey.value, contextType.value, permission => authStore.hasPermission(permission)))

const panelComponents = {
  users: markRaw(PlatformUsersPanel),
  'identity-changes': markRaw(IdentityChangesPanel),
  'user-accounts': markRaw(TenantUserAccountsPanel),
  invitations: markRaw(TenantInvitationsPanel),
  tenants: markRaw(PlatformTenantsPanel),
  departments: markRaw(DepartmentsPanel),
  'project-groups': markRaw(ProjectGroupsPanel),
  roles: markRaw(TenantRolesPanel),
  'role-assignments': markRaw(TenantRoleAssignmentsPanel),
  'oauth-clients': markRaw(OAuthClientsPanel),
  'service-principal-accounts': markRaw(ServicePrincipalAccountsPanel),
  'api-consumers': markRaw(APIConsumersPanel),
  'security-policy': markRaw(SecurityPolicy),
  audit: markRaw(AuditPanel)
}

const panelIcons = {
  users: User,
  'identity-changes': DocumentChecked,
  'user-accounts': User,
  invitations: Tickets,
  tenants: OfficeBuilding,
  departments: Connection,
  'project-groups': Share,
  roles: DocumentChecked,
  'role-assignments': UserFilled,
  'oauth-clients': Key,
  'service-principal-accounts': Connection,
  'api-consumers': Key,
  'security-policy': Setting,
  audit: Bell
}

function tabIcon(panel) {
  return panelIcons[panel] || DocumentChecked
}

async function restoreTabFromRoute() {
  const routeState = resolveIAMCategoryRouteState(availableTabs.value.map(tab => tab.key), route.query)
  activeTab.value = routeState.activeTab
  if (routeState.changed && pageDefinition.value) {
    await navigateSystemRoute(router, { name: pageDefinition.value.routeName, query: routeState.query }, { history: 'replace' })
  }
}

async function selectTab(tab) {
  const tabKey = String(tab || '')
  const defaultTab = availableTabs.value[0]?.key || ''
  if (!availableTabs.value.some(item => item.key === tabKey) || !pageDefinition.value) return
  activeTab.value = tabKey
  await navigateSystemRoute(router, {
    name: pageDefinition.value.routeName,
    query: tabKey === defaultTab ? {} : { tab: tabKey }
  }, { history: 'replace' })
}

async function openRoleAssignments() {
  await navigateSystemRoute(router, { name: 'IAMRoles', query: { tab: 'role-assignments' } })
}

watch([availableTabs, () => route.query, pageKey], restoreTabFromRoute, { immediate: true })
</script>

<style>
.iam-category-page { min-width: 0; color: var(--addp-text-primary); }
.iam-page-header { margin-bottom: 12px; }
.iam-page-header h2 { margin: 0; font-size: 22px; font-weight: 600; letter-spacing: 0; }
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
