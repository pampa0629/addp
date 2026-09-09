<template>
  <section class="iam-panel">
    <div v-if="showRecommendations" class="iam-role-recommendations">
      <div>
        <strong>{{ t('system.iam.roleAssignments.recommendations.title') }}</strong>
        <span>{{ t('system.iam.roleAssignments.recommendations.description') }}</span>
      </div>
      <div class="iam-role-recommendations__actions">
        <el-button
          v-for="recommendation in TENANT_ROLE_RECOMMENDATIONS"
          :key="recommendation.roleKey"
          :disabled="recommendationAssigned(recommendation.roleKey)"
          :icon="recommendationAssigned(recommendation.roleKey) ? Check : recommendationIcons[recommendation.roleKey]"
          @click="openCreate(recommendation.roleKey)"
        >
          {{ t(recommendation.labelKey) }}
          <span v-if="recommendationAssigned(recommendation.roleKey)" class="iam-role-recommendations__assigned">
            {{ t('system.iam.roleAssignments.assigned') }}
          </span>
        </el-button>
      </div>
    </div>

    <div class="iam-toolbar">
      <div class="iam-filters">
        <TenantMemberSelect
          v-if="!fixedMembership"
          v-model="filters.membership_id"
          :members="membershipOptions"
          :current-membership-id="currentMembershipID"
          :placeholder="t('system.iam.roleAssignments.allMembers')"
          @change="reload"
        />
        <el-select v-model="filters.scope_type" :placeholder="t('system.iam.roleAssignments.scope')" clearable @change="reload">
          <el-option v-for="scope in scopeTypes" :key="scope" :label="scopeLabel(scope)" :value="scope" />
        </el-select>
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload">
          <el-option v-for="status in assignmentStatuses" :key="status" :label="t(`system.iam.status.${status}`)" :value="status" />
        </el-select>
        <el-button v-if="!fixedMembership" :type="showingCurrentAccount ? 'primary' : 'default'" :icon="User" :disabled="!currentMembershipID" @click="toggleCurrentAccountFilter">
          {{ t('system.iam.memberships.currentAccount') }}
        </el-button>
        <el-button :icon="Refresh" @click="load">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="!props.readOnly && (!fixedMember || fixedMember.status === 'active') && can('iam.tenant_role_assignment.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.roleAssignments.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column v-if="!fixedMembership" :label="t('system.iam.memberships.member')" min-width="250"><template #default="{ row }"><TenantMemberIdentity :member="row" :current-membership-id="currentMembershipID" /></template></el-table-column>
      <el-table-column :label="t('system.iam.roles.role')" min-width="210"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ assignmentRoleName(row) }}</strong><span class="iam-role-key">{{ row.role_key }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.roleAssignments.scope')" width="170"><template #default="{ row }">{{ scopeValue(row) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="110"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'">{{ t(`system.iam.status.${row.status}`) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.roleAssignments.validUntil')" width="180"><template #default="{ row }">{{ formatDate(row.valid_until) }}</template></el-table-column>
      <el-table-column v-if="!props.readOnly" :label="t('system.iam.common.actions')" width="110" fixed="right"><template #default="{ row }"><el-button v-if="can('iam.tenant_role_assignment.revoke') && row.status === 'active'" link type="danger" :icon="CircleClose" @click="revoke(row)">{{ t('system.iam.common.revoke') }}</el-button></template></el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="dialogVisible" :title="t('system.iam.roleAssignments.create')" width="min(620px, calc(100% - 24px))">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="t('system.iam.memberships.member')" prop="membershipId">
          <TenantMemberIdentity v-if="fixedMembership" :member="fixedMember" />
          <TenantMemberSelect v-else v-model="form.membershipId" :members="membershipOptions" :current-membership-id="currentMembershipID" active-only />
        </el-form-item>
        <el-form-item :label="t('system.iam.roleAssignments.scope')" prop="scopeType">
          <el-select v-model="form.scopeType" style="width: 100%"><el-option v-for="scope in availableScopeTypes" :key="scope" :label="scopeLabel(scope)" :value="scope" /></el-select>
        </el-form-item>
        <el-form-item v-if="form.scopeType === 'department'" :label="t('system.iam.roleAssignments.departmentId')" prop="departmentId"><el-input v-model="form.departmentId" /></el-form-item>
        <el-form-item v-if="form.scopeType === 'project_group'" :label="t('system.iam.roleAssignments.projectGroupId')" prop="projectGroupId"><el-input v-model="form.projectGroupId" /></el-form-item>
        <el-form-item :label="t('system.iam.roles.role')" prop="roleIds">
          <el-select
            v-model="form.roleIds"
            :disabled="!roleSelectionReady || assignmentsLoading"
            :loading="assignmentsLoading"
            :no-data-text="t('system.iam.roleAssignments.noAssignableRoles')"
            multiple
            collapse-tags
            collapse-tags-tooltip
            :max-collapse-tags="3"
            :multiple-limit="MAX_ROLE_ASSIGNMENT_BATCH_SIZE"
            filterable
            style="width: 100%"
          >
            <el-option v-for="role in roleOptions" :key="role.id" :label="roleLabel(role)" :value="role.id" :disabled="role.assigned">
              <span class="iam-role-option">
                <span class="iam-role-option__name">{{ roleLabel(role) }}</span>
                <span class="iam-role-option__meta">
                  <span v-if="role.assigned" class="iam-role-option__assigned"><el-icon><Check /></el-icon>{{ t('system.iam.roleAssignments.assigned') }}</span>
                  <span v-else-if="role.assignedElsewhere">{{ t('system.iam.roleAssignments.assignedElsewhere') }}</span>
                  <span class="iam-role-option-key">{{ role.role_key }}</span>
                </span>
              </span>
            </el-option>
          </el-select>
          <div v-if="roleSelectionReady && !assignmentsLoading && availableRoleCount === 0" class="iam-role-selection-detail">{{ t('system.iam.roleAssignments.allAssigned') }}</div>
          <div v-else-if="form.roleIds.length" class="iam-role-selection-detail">{{ t('system.iam.roleAssignments.selectedCount', { count: form.roleIds.length }) }}</div>
        </el-form-item>
        <el-form-item v-if="!hasTenantAdministratorSelected" :label="t('system.iam.roleAssignments.validUntil')"><el-date-picker v-model="form.validUntil" type="datetime" clearable style="width: 100%" /></el-form-item>
        <el-form-item :label="t('system.iam.common.reason')"><el-input v-model="form.reason" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :disabled="!canSubmitAssignments" :loading="submitting" @click="submit">{{ t('system.iam.roleAssignments.confirmAssignment') }}</el-button></template>
    </el-dialog>
    <MFAStepUpDialog ref="stepUpRef" />
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, CircleClose, Connection, DataAnalysis, Plus, Refresh, User, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import MFAStepUpDialog from './MFAStepUpDialog.vue'
import TenantMemberIdentity from './TenantMemberIdentity.vue'
import TenantMemberSelect from './TenantMemberSelect.vue'
import {
  TENANT_ADMINISTRATOR_ROLE_KEY,
  TENANT_ROLE_RECOMMENDATIONS,
  buildTenantRoleOptions,
  formatTenantAssignmentScope,
  hasTenantRole,
  resolveRoleName,
  resolveTenantScopeLabel,
  tenantRoleKeys
} from '../../utils/iamRoles'

const props = defineProps({
  fixedMembership: { type: Object, default: null },
  readOnly: { type: Boolean, default: false }
})

const { t, te } = useI18n()
const MAX_ROLE_ASSIGNMENT_BATCH_SIZE = 50
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const scopeTypes = ['tenant', 'department', 'project_group']
const assignmentStatuses = ['active', 'revoked']
const rows = ref([])
const roles = ref([])
const membershipOptions = ref([])
const memberAssignments = ref([])
const loading = ref(false)
const assignmentsLoading = ref(false)
const submitting = ref(false)
const stepUpRef = ref()
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const dialogVisible = ref(false)
const formRef = ref()
const filters = reactive({ membership_id: '', scope_type: '', status: '' })
const form = reactive({ membershipId: '', roleIds: [], scopeType: 'tenant', departmentId: '', projectGroupId: '', validUntil: null, reason: '' })
const fixedMembership = computed(() => props.fixedMembership)
const fixedMember = computed(() => fixedMembership.value ? {
  id: fixedMembership.value.membership_id,
  principal_id: fixedMembership.value.id,
  principal_type: 'service_principal',
  display_name: fixedMembership.value.name,
  status: fixedMembership.value.membership_status
} : null)
const effectiveMembershipID = computed(() => fixedMembership.value?.membership_id || filters.membership_id || '')
const currentMembershipID = computed(() => authStore.authContext?.context?.tenant_membership_id || '')
const showingCurrentAccount = computed(() => Boolean(currentMembershipID.value && String(filters.membership_id) === String(currentMembershipID.value)))
const activeMembers = computed(() => membershipOptions.value.filter((member) => member.status === 'active'))
const selectedMember = computed(() => activeMembers.value.find((member) => String(member.id) === String(form.membershipId)))
const selectedRoles = computed(() => roles.value.filter((role) => form.roleIds.includes(role.id)))
const compatibleRoles = computed(() => roles.value.filter((role) =>
  (role.allowed_principal_types || []).includes(selectedMember.value?.principal_type)
))
const availableScopeTypes = computed(() => scopeTypes.filter((scope) => compatibleRoles.value.some((role) => (role.allowed_scope_types || []).includes(scope))))
const roleSelectionReady = computed(() => Boolean(selectedMember.value?.principal_type && form.scopeType &&
  (form.scopeType !== 'department' || form.departmentId.trim()) &&
  (form.scopeType !== 'project_group' || form.projectGroupId.trim())))
const roleOptions = computed(() => roleSelectionReady.value ? buildTenantRoleOptions(roles.value, memberAssignments.value, {
  principalType: selectedMember.value.principal_type,
  scopeType: form.scopeType,
  departmentId: form.departmentId,
  projectGroupId: form.projectGroupId
}) : [])
const selectedRoleOptions = computed(() => roleOptions.value.filter((role) => form.roleIds.includes(role.id)))
const availableRoleCount = computed(() => roleOptions.value.filter((role) => !role.assigned).length)
const hasTenantAdministratorSelected = computed(() => selectedRoles.value.some((role) => role.role_key === TENANT_ADMINISTRATOR_ROLE_KEY))
const canSubmitAssignments = computed(() => form.roleIds.length > 0 && selectedRoleOptions.value.length === form.roleIds.length && selectedRoleOptions.value.every((role) => !role.assigned))
const showRecommendations = computed(() => !fixedMembership.value && can('iam.tenant_role_assignment.create') && tenantRoleKeys(authStore.authContext).includes(TENANT_ADMINISTRATOR_ROLE_KEY))
const recommendationIcons = {
  'tenant.infrastructure_administrator': Connection,
  'tenant.data_viewer': View,
  'tenant.data_steward': DataAnalysis
}
const rules = computed(() => ({
  membershipId: [{ required: true, message: t('system.iam.validation.required'), trigger: 'change' }],
  roleIds: [{ required: true, type: 'array', min: 1, message: t('system.iam.validation.required'), trigger: 'change' }],
  scopeType: [{ required: true, message: t('system.iam.validation.required'), trigger: 'change' }],
  departmentId: [{ required: form.scopeType === 'department', message: t('system.iam.validation.required'), trigger: 'blur' }],
  projectGroupId: [{ required: form.scopeType === 'project_group', message: t('system.iam.validation.required'), trigger: 'blur' }]
}))

function roleLabel(role) { return resolveRoleName(role, t, te) }
function assignmentRoleName(row) { return resolveRoleName(row, t, te) }
function recommendationAssigned(roleKey) { return hasTenantRole(authStore.authContext, roleKey, 'tenant') }
function scopeLabel(scope) { return resolveTenantScopeLabel(scope, t) }
function scopeValue(row) { return formatTenantAssignmentScope(row, t) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.tenantRoleAssignments.list({
      page: page.value,
      page_size: pageSize.value,
      membership_id: effectiveMembershipID.value || undefined,
      principal_type: fixedMembership.value ? 'service_principal' : 'user',
      scope_type: filters.scope_type || undefined,
      status: filters.status || undefined
    })
    rows.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}
async function loadOptions() {
  if (fixedMembership.value) {
    roles.value = await iamAPI.tenantRoles.list() || []
    membershipOptions.value = fixedMember.value ? [fixedMember.value] : []
    return
  }
  const [roleRows, memberRows] = await Promise.all([
    iamAPI.tenantRoles.list(),
    iamAPI.memberships.listAll({ principal_type: 'user' })
  ])
  roles.value = roleRows || []
  membershipOptions.value = memberRows || []
}
async function loadFilterOptions() {
  try {
    await loadOptions()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  }
}
let assignmentRequest = 0
async function loadMemberAssignments() {
  const membershipId = form.membershipId
  const request = ++assignmentRequest
  memberAssignments.value = []
  if (!membershipId) return
  assignmentsLoading.value = true
  try {
    const collected = []
    let assignmentPage = 1
    let totalPages = 1
    do {
      const result = await iamAPI.tenantRoleAssignments.list({
        membership_id: membershipId,
        status: 'active',
        page: assignmentPage,
        page_size: 100
      })
      collected.push(...(result.data || []))
      totalPages = result.total_pages || 1
      assignmentPage += 1
    } while (assignmentPage <= totalPages)
    if (request === assignmentRequest) memberAssignments.value = collected
  } catch (error) {
    if (request === assignmentRequest) ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    if (request === assignmentRequest) assignmentsLoading.value = false
  }
}
function reload() { page.value = 1; return load() }
function toggleCurrentAccountFilter() {
  const wasShowingCurrentAccount = showingCurrentAccount.value
  filters.membership_id = wasShowingCurrentAccount ? '' : currentMembershipID.value
  return reload()
}
async function openCreate(roleKey = '') {
  Object.assign(form, { membershipId: '', roleIds: [], scopeType: 'tenant', departmentId: '', projectGroupId: '', validUntil: null, reason: '' })
  try {
    await loadOptions()
    if (fixedMembership.value) form.membershipId = fixedMembership.value.membership_id
    else if (activeMembers.value.some((member) => String(member.id) === String(currentMembershipID.value))) form.membershipId = currentMembershipID.value
    await loadMemberAssignments()
    const recommendedRole = roles.value.find((role) => role.role_key === roleKey)
    const recommendedOption = roleOptions.value.find((role) => role.id === recommendedRole?.id)
    if (recommendedOption && !recommendedOption.assigned) {
      form.roleIds = [recommendedRole.id]
    }
    dialogVisible.value = true
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  }
}
async function submit() {
  await formRef.value?.validate()
  submitting.value = true
  const payload = {
    membership_id: form.membershipId,
    role_ids: form.roleIds,
    scope_type: form.scopeType,
    department_id: form.scopeType === 'department' ? form.departmentId : null,
    project_group_id: form.scopeType === 'project_group' ? form.projectGroupId : null,
    valid_until: form.validUntil ? form.validUntil.toISOString() : null,
    reason: form.reason.trim()
  }
  try {
    const assignments = await createAssignmentsWithStepUp(payload)
    await refreshCurrentMemberAuthorization(form.membershipId)
    ElMessage.success(t('system.iam.roleAssignments.assignedCount', { count: assignments.length }))
    dialogVisible.value = false
    await load()
  } catch (error) {
    if (error !== false) ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally {
    submitting.value = false
  }
}
async function createAssignmentsWithStepUp(payload) {
  try {
    return await iamAPI.tenantRoleAssignments.create(payload)
  } catch (error) {
    if (error.response?.data?.error_code !== 'step_up_required') throw error
    const steppedUp = await stepUpRef.value?.request()
    if (!steppedUp) throw false
    return iamAPI.tenantRoleAssignments.create(payload)
  }
}
async function revoke(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t('system.iam.common.revoke'), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.tenantRoleAssignments.revoke(row.id, value.trim())
    await refreshCurrentMemberAuthorization(row.membership_id)
    ElMessage.success(t('system.iam.common.updated'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  }
}
async function refreshCurrentMemberAuthorization(membershipId) {
  if (String(membershipId) !== String(authStore.authContext?.context?.tenant_membership_id || '')) return
  await authStore.refreshAuthorization()
}
watch(() => form.membershipId, () => {
  form.roleIds = []
  form.validUntil = null
  if (dialogVisible.value) loadMemberAssignments()
})
watch(() => [form.scopeType, form.departmentId, form.projectGroupId], () => {
  form.roleIds = []
  form.validUntil = null
})
watch(() => form.roleIds, () => {
  if (hasTenantAdministratorSelected.value) form.validUntil = null
}, { deep: true })
onMounted(() => {
  load()
  if (!fixedMembership.value) loadFilterOptions()
})
watch(() => fixedMembership.value?.membership_id, () => {
  page.value = 1
  dialogVisible.value = false
  load()
})
</script>

<style scoped>
.iam-role-recommendations { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-width: 0; padding: 12px 14px; margin: 0 0 16px; border-bottom: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); }
.iam-role-recommendations > div:first-child { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.iam-role-recommendations strong { color: var(--addp-text-primary); font-size: 14px; font-weight: 600; }
.iam-role-recommendations span { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
.iam-role-recommendations__actions { display: flex; flex: 0 0 auto; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.iam-role-recommendations__actions .el-button { margin-left: 0; }
.iam-role-recommendations__assigned { margin-left: 6px; color: var(--addp-success-color); font-size: 12px; }
.iam-role-option { display: flex; align-items: center; justify-content: space-between; gap: 18px; width: 100%; min-width: 0; }
.iam-role-option__name { overflow: hidden; min-width: 0; text-overflow: ellipsis; white-space: nowrap; }
.iam-role-option__meta { display: inline-flex; align-items: center; flex: 0 1 auto; gap: 10px; min-width: 0; color: var(--addp-text-tertiary); font-size: 12px; }
.iam-role-option__assigned { display: inline-flex; align-items: center; gap: 4px; color: var(--addp-success-color); }
.iam-role-option-key { overflow-wrap: anywhere; }
.iam-role-key { overflow-wrap: anywhere; }
.iam-role-selection-detail { width: 100%; margin-top: 6px; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
@media (max-width: 900px) {
  .iam-role-recommendations { align-items: stretch; flex-direction: column; }
  .iam-role-recommendations__actions { justify-content: flex-start; }
}
@media (max-width: 620px) {
  .iam-role-recommendations__actions { flex-direction: column; }
  .iam-role-recommendations__actions .el-button { width: 100%; }
  .iam-role-key { display: none; }
}
</style>
