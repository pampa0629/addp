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
      <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      <el-button v-if="can('iam.tenant_role_assignment.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.roleAssignments.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.memberships.member')" min-width="220"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ memberDisplayName(row) }}</strong><span>{{ memberPrincipalLabel(row) }} · {{ memberIdentifier(row) }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.roles.role')" min-width="210"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ assignmentRoleName(row) }}</strong><span class="iam-role-key">{{ row.role_key }}</span></div></template></el-table-column>
      <el-table-column :label="t('system.iam.roleAssignments.scope')" width="170"><template #default="{ row }">{{ scopeValue(row) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="110"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'">{{ t(`system.iam.status.${row.status}`) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.roleAssignments.validUntil')" width="180"><template #default="{ row }">{{ formatDate(row.valid_until) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="110" fixed="right"><template #default="{ row }"><el-button v-if="can('iam.tenant_role_assignment.revoke') && row.status === 'active'" link type="danger" :icon="CircleClose" @click="revoke(row)">{{ t('system.iam.common.revoke') }}</el-button></template></el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="dialogVisible" :title="t('system.iam.roleAssignments.create')" width="min(620px, calc(100% - 24px))">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="t('system.iam.memberships.member')" prop="membershipId">
          <el-select v-model="form.membershipId" filterable style="width: 100%"><el-option v-for="member in members" :key="member.id" :label="memberLabel(member)" :value="member.id" /></el-select>
        </el-form-item>
        <el-form-item :label="t('system.iam.roleAssignments.scope')" prop="scopeType">
          <el-select v-model="form.scopeType" style="width: 100%"><el-option v-for="scope in availableScopeTypes" :key="scope" :label="scopeLabel(scope)" :value="scope" /></el-select>
        </el-form-item>
        <el-form-item v-if="form.scopeType === 'department'" :label="t('system.iam.roleAssignments.departmentId')" prop="departmentId"><el-input v-model="form.departmentId" /></el-form-item>
        <el-form-item v-if="form.scopeType === 'project_group'" :label="t('system.iam.roleAssignments.projectGroupId')" prop="projectGroupId"><el-input v-model="form.projectGroupId" /></el-form-item>
        <el-form-item :label="t('system.iam.roles.role')" prop="roleId">
          <el-select
            v-model="form.roleId"
            :disabled="!roleSelectionReady || assignmentsLoading"
            :loading="assignmentsLoading"
            :no-data-text="t('system.iam.roleAssignments.noAssignableRoles')"
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
          <div v-else-if="selectedRoleDescription" class="iam-role-selection-detail">{{ selectedRoleDescription }}</div>
        </el-form-item>
        <el-form-item v-if="selectedRole?.role_key !== 'tenant.administrator'" :label="t('system.iam.roleAssignments.validUntil')"><el-date-picker v-model="form.validUntil" type="datetime" clearable style="width: 100%" /></el-form-item>
        <el-form-item :label="t('system.iam.common.reason')"><el-input v-model="form.reason" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :disabled="!selectedRoleOption || selectedRoleOption.assigned" :loading="submitting" @click="submit">{{ t('system.iam.common.create') }}</el-button></template>
    </el-dialog>
    <MFAStepUpDialog ref="stepUpRef" />
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, CircleClose, Connection, DataAnalysis, Plus, Refresh, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import MFAStepUpDialog from './MFAStepUpDialog.vue'
import {
  TENANT_ADMINISTRATOR_ROLE_KEY,
  TENANT_ROLE_RECOMMENDATIONS,
  buildTenantRoleOptions,
  formatTenantAssignmentScope,
  hasTenantRole,
  resolveRoleDescription,
  resolveRoleName,
  resolveTenantScopeLabel,
  tenantRoleKeys
} from '../../utils/iamRoles'

const { t, te } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const rows = ref([])
const roles = ref([])
const members = ref([])
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
const form = reactive({ membershipId: '', roleId: '', scopeType: 'tenant', departmentId: '', projectGroupId: '', validUntil: null, reason: '' })
const selectedMember = computed(() => members.value.find((member) => member.id === form.membershipId))
const selectedRole = computed(() => roles.value.find((role) => role.id === form.roleId))
const compatibleRoles = computed(() => roles.value.filter((role) =>
  (role.allowed_principal_types || []).includes(selectedMember.value?.principal_type)
))
const availableScopeTypes = computed(() => ['tenant', 'department', 'project_group'].filter((scope) => compatibleRoles.value.some((role) => (role.allowed_scope_types || []).includes(scope))))
const roleSelectionReady = computed(() => Boolean(selectedMember.value?.principal_type && form.scopeType &&
  (form.scopeType !== 'department' || form.departmentId.trim()) &&
  (form.scopeType !== 'project_group' || form.projectGroupId.trim())))
const roleOptions = computed(() => roleSelectionReady.value ? buildTenantRoleOptions(roles.value, memberAssignments.value, {
  principalType: selectedMember.value.principal_type,
  scopeType: form.scopeType,
  departmentId: form.departmentId,
  projectGroupId: form.projectGroupId
}) : [])
const selectedRoleOption = computed(() => roleOptions.value.find((role) => role.id === form.roleId))
const availableRoleCount = computed(() => roleOptions.value.filter((role) => !role.assigned).length)
const selectedRoleDescription = computed(() => selectedRole.value ? resolveRoleDescription(selectedRole.value, t, te) : '')
const showRecommendations = computed(() => can('iam.tenant_role_assignment.create') && tenantRoleKeys(authStore.authContext).includes(TENANT_ADMINISTRATOR_ROLE_KEY))
const recommendationIcons = {
  'tenant.infrastructure_administrator': Connection,
  'tenant.data_viewer': View,
  'tenant.data_steward': DataAnalysis
}
const rules = computed(() => ({
  membershipId: [{ required: true, message: t('system.iam.validation.required'), trigger: 'change' }],
  roleId: [{ required: true, message: t('system.iam.validation.required'), trigger: 'change' }],
  scopeType: [{ required: true, message: t('system.iam.validation.required'), trigger: 'change' }],
  departmentId: [{ required: form.scopeType === 'department', message: t('system.iam.validation.required'), trigger: 'blur' }],
  projectGroupId: [{ required: form.scopeType === 'project_group', message: t('system.iam.validation.required'), trigger: 'blur' }]
}))

function roleLabel(role) { return resolveRoleName(role, t, te) }
function assignmentRoleName(row) { return resolveRoleName(row, t, te) }
function recommendationAssigned(roleKey) { return hasTenantRole(authStore.authContext, roleKey, 'tenant') }
function memberLabel(member) { return `${member.display_name} (${member.username || member.principal_id})` }
function memberDisplayName(row) { return row.display_name || row.service_principal_name || row.principal_id }
function memberPrincipalLabel(row) { return t(`system.iam.principalType.${row.principal_type || 'user'}`) }
function memberIdentifier(row) { return row.username || row.service_principal_name || row.principal_id }
function scopeLabel(scope) { return resolveTenantScopeLabel(scope, t) }
function scopeValue(row) { return formatTenantAssignmentScope(row, t) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.tenantRoleAssignments.list({ page: page.value, page_size: pageSize.value })
    rows.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}
async function loadOptions() {
  const [roleRows, memberResult] = await Promise.all([
    iamAPI.tenantRoles.list(),
    iamAPI.memberships.list({ page: 1, page_size: 100, status: 'active' })
  ])
  roles.value = roleRows || []
  members.value = memberResult.data || []
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
async function openCreate(roleKey = '') {
  Object.assign(form, { membershipId: '', roleId: '', scopeType: 'tenant', departmentId: '', projectGroupId: '', validUntil: null, reason: '' })
  try {
    await loadOptions()
    const currentMembershipID = authStore.authContext?.context?.tenant_membership_id
    if (members.value.some((member) => member.id === currentMembershipID)) form.membershipId = currentMembershipID
    await loadMemberAssignments()
    const recommendedRole = roles.value.find((role) => role.role_key === roleKey)
    const recommendedOption = roleOptions.value.find((role) => role.id === recommendedRole?.id)
    if (recommendedOption && !recommendedOption.assigned) {
      form.roleId = recommendedRole.id
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
    role_id: form.roleId,
    scope_type: form.scopeType,
    department_id: form.scopeType === 'department' ? form.departmentId : null,
    project_group_id: form.scopeType === 'project_group' ? form.projectGroupId : null,
    valid_until: form.validUntil ? form.validUntil.toISOString() : null,
    reason: form.reason.trim()
  }
  try {
    await createAssignmentWithStepUp(payload)
    await refreshCurrentMemberAuthorization(form.membershipId)
    ElMessage.success(t('system.iam.common.saved'))
    dialogVisible.value = false
    await load()
  } catch (error) {
    if (error !== false) ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally {
    submitting.value = false
  }
}
async function createAssignmentWithStepUp(payload) {
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
  if (membershipId !== authStore.authContext?.context?.tenant_membership_id) return
  await authStore.refreshAuthorization()
}
watch(() => form.membershipId, () => {
  form.roleId = ''
  form.validUntil = null
  if (dialogVisible.value) loadMemberAssignments()
})
watch(() => [form.scopeType, form.departmentId, form.projectGroupId], () => {
  form.roleId = ''
  form.validUntil = null
})
onMounted(load)
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
