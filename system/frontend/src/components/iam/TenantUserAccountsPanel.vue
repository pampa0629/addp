<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload"><el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" /></el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.tenant_invitation.read') && can('iam.tenant_invitation.create')" type="primary" :icon="Plus" @click="openInvitations">{{ t('system.iam.memberships.inviteUser') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.tabs.userAccounts')" min-width="320">
        <template #default="{ row }">
          <div class="iam-primary-cell"><strong>{{ row.display_name }} <el-tag v-if="isCurrentMembership(row)" size="small" effect="plain">{{ t('system.iam.memberships.currentAccount') }}</el-tag></strong><span>{{ row.username || row.principal_id }}</span></div>
          <div v-if="can('iam.tenant_role_assignment.read')" class="iam-account-roles">
            <span v-if="roleSummaries[row.id]?.state === 'loading'" class="iam-role-hint">{{ t('common.loading') }}</span>
            <el-button v-else-if="roleSummaries[row.id]?.state === 'error'" link type="primary" :icon="Refresh" @click="loadRoleSummary(row.id)">{{ t('system.iam.memberships.roleSummaryLoadFailed') }}</el-button>
            <template v-else-if="roleSummaries[row.id]?.state === 'ready'">
              <span v-if="!roleSummaries[row.id].roles.length" class="iam-role-hint">{{ t('system.iam.memberships.noEffectiveRoles') }}</span>
              <template v-else>
                <el-button link type="primary" @click="openRoles(row)">{{ t('system.iam.memberships.effectiveRoleCount', { count: roleSummaries[row.id].roles.length }) }}</el-button>
                <div class="iam-account-role-tags" :title="roleSummaries[row.id].roles.map(roleName).join('、')">
                  <el-tag v-for="role in roleSummaries[row.id].roles.slice(0, 2)" :key="role.role_id" size="small" effect="plain">{{ roleName(role) }}</el-tag>
                  <el-tag v-if="roleSummaries[row.id].roles.length > 2" size="small" effect="plain">+{{ roleSummaries[row.id].roles.length - 2 }}</el-tag>
                </div>
              </template>
            </template>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.memberships.source')" width="150"><template #default="{ row }">{{ sourceLabel(row.source_type) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.joinedAt')" width="180"><template #default="{ row }">{{ formatDate(row.joined_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.memberships.expiresAt')" width="180"><template #default="{ row }">{{ formatDate(row.expires_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="280" fixed="right">
        <template #default="{ row }">
          <div class="iam-account-actions">
            <el-button v-if="can('iam.tenant_role_assignment.read')" link type="primary" :icon="UserFilled" @click="openRoles(row)">{{ t(canManageRoles(row) ? 'system.iam.memberships.manageRoles' : 'system.iam.memberships.viewRoles') }}</el-button>
            <el-button v-if="can('iam.tenant_membership.update') && row.status !== 'ended'" link type="primary" :icon="Edit" @click="openExpiry(row)">{{ t('system.iam.common.edit') }}</el-button>
            <el-dropdown v-if="statusActions(row).length" trigger="click" placement="bottom-end" @command="changeStatus(row, $event)">
              <el-button link :icon="MoreFilled">{{ t('common.more') }}</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-for="action in statusActions(row)" :key="action.command" :command="action.command" :divided="action.command === 'close' && statusActions(row).length > 1">
                    <el-icon :class="`iam-account-action--${action.tone}`"><component :is="action.icon" /></el-icon>
                    <span :class="`iam-account-action--${action.tone}`">{{ t(`system.iam.common.${action.command}`) }}</span>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="expiryVisible" :title="t('system.iam.memberships.editExpiry')" width="480px">
      <el-form label-position="top"><el-form-item :label="t('system.iam.memberships.expiresAt')"><el-date-picker v-model="expiryDate" type="datetime" clearable /></el-form-item></el-form>
      <template #footer><el-button @click="expiryVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="saveExpiry">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>

    <el-drawer v-model="rolesVisible" :title="t('system.iam.memberships.roleDrawerTitle', { name: selectedAccount?.display_name || '' })" size="min(960px, calc(100% - 24px))" destroy-on-close>
      <TenantRoleAssignmentsPanel
        v-if="selectedAccount"
        :fixed-membership="selectedAccount"
        :read-only="!canManageRoles(selectedAccount)"
        @assignments-changed="loadRoleSummary"
      />
    </el-drawer>
  </section>
</template>

<script setup>
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, MoreFilled, Plus, Refresh, RefreshLeft, Search, UserFilled, VideoPause } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { resolveMembershipSourceLabel } from '../../utils/iamPresentation'
import { resolveRoleName, summarizeEffectiveTenantRoles } from '../../utils/iamRoles'
import { navigateSystemRoute } from '../../utils/moduleNavigation'
import TenantRoleAssignmentsPanel from './TenantRoleAssignmentsPanel.vue'

const { t, te } = useI18n()
const authStore = useAuthStore()
const router = useRouter()
const can = (permission) => authStore.hasPermission(permission)
const statuses = ['active', 'suspended', 'ended']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const expiryVisible = ref(false)
const expiryRow = ref(null)
const expiryDate = ref(null)
const rolesVisible = ref(false)
const selectedAccount = ref(null)
const roleSummaries = ref({})
const roleRequests = new Map()
let roleRequest = 0
let membershipRequest = 0

function roleName(role) { return resolveRoleName(role, t, te) }
async function loadRoleSummary(membershipId) {
  if (!can('iam.tenant_role_assignment.read') || !rows.value.some((row) => row.id === membershipId)) return
  const request = ++roleRequest
  roleRequests.set(membershipId, request)
  roleSummaries.value[membershipId] = { state: 'loading', roles: [] }
  try {
    const assignments = await iamAPI.tenantRoleAssignments.listAll({ membership_id: membershipId, principal_type: 'user', effective_state: 'effective' })
    if (roleRequests.get(membershipId) === request) roleSummaries.value[membershipId] = { state: 'ready', roles: summarizeEffectiveTenantRoles(assignments) }
  } catch {
    if (roleRequests.get(membershipId) === request) roleSummaries.value[membershipId] = { state: 'error', roles: [] }
  }
}

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function sourceLabel(sourceType) { return resolveMembershipSourceLabel(sourceType, t, te) }
function isCurrentMembership(row) { return row.id === authStore.authContext?.context?.tenant_membership_id }
function canManageRoles(row) { return row.status !== 'ended' && (can('iam.tenant_role_assignment.create') || can('iam.tenant_role_assignment.revoke')) }
function statusActions(row) {
  const actions = []
  if (can('iam.tenant_membership.suspend') && row.status === 'active') actions.push({ command: 'suspend', icon: VideoPause, tone: 'warning' })
  if (can('iam.tenant_membership.restore') && row.status === 'suspended') actions.push({ command: 'restore', icon: RefreshLeft, tone: 'success' })
  if (can('iam.tenant_membership.close') && row.status !== 'ended') actions.push({ command: 'close', icon: CircleClose, tone: 'danger' })
  return actions
}
function statusType(status) { return ({ active: 'success', suspended: 'warning', ended: 'info' })[status] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  const request = ++membershipRequest
  loading.value = true
  roleRequests.clear()
  roleSummaries.value = {}
  try {
    const result = await iamAPI.memberships.list({ page: page.value, page_size: pageSize.value, principal_type: 'user', ...filters })
    if (request !== membershipRequest) return
    rows.value = result.data || []; total.value = result.total || 0
    loading.value = false
    await Promise.all(rows.value.map((row) => loadRoleSummary(row.id)))
  } catch (error) { if (request === membershipRequest) ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { if (request === membershipRequest) loading.value = false }
}
function reload() { page.value = 1; return load() }
function openInvitations() { return navigateSystemRoute(router, { name: 'IAMAccounts', query: { tab: 'invitations' } }) }
function openRoles(row) { selectedAccount.value = row; rolesVisible.value = true }
function openExpiry(row) { expiryRow.value = row; expiryDate.value = row.expires_at ? new Date(row.expires_at) : null; expiryVisible.value = true }
async function saveExpiry() {
  submitting.value = true
  try {
    await iamAPI.memberships.update(expiryRow.value.id, expiryDate.value ? expiryDate.value.toISOString() : null)
    ElMessage.success(t('system.iam.common.saved')); expiryVisible.value = false; await load()
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed')) }
  finally { submitting.value = false }
}
async function changeStatus(row, action) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t(`system.iam.common.${action}`), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'),
      type: action === 'close' ? 'warning' : 'info'
    })
    await iamAPI.memberships[action](row.id, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}
onMounted(load)
onBeforeUnmount(() => { membershipRequest += 1; roleRequests.clear() })
</script>

<style scoped>
.iam-account-actions { display: flex; align-items: center; flex-wrap: wrap; column-gap: 12px; row-gap: 6px; }
.iam-account-actions > .el-button { margin-left: 0; }
.iam-account-action--warning { color: var(--el-color-warning); }
.iam-account-action--success { color: var(--el-color-success); }
.iam-account-action--danger { color: var(--el-color-danger); }
.iam-account-roles { margin-top: 6px; display: flex; flex-direction: column; align-items: flex-start; gap: 5px; }
.iam-role-hint { font-size: 12px; color: var(--addp-text-secondary); }
.iam-account-role-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.iam-account-role-tags :deep(.el-tag__content) { max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
