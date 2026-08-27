<template>
  <el-dialog v-model="dialogVisible" :title="`${t('system.iam.organization.members')} · ${organization?.name || ''}`" width="820px" destroy-on-close>
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-select v-model="status" clearable :placeholder="t('system.iam.common.status')" @change="reload">
          <el-option v-for="item in ['active', 'ended']" :key="item" :label="statusLabel(item)" :value="item" />
        </el-select>
        <el-button :icon="Refresh" @click="load">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.organization.addMember') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.memberships.member')" min-width="190">
        <template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.display_name }}</strong><span>{{ row.username || row.principal_id }}</span></div></template>
      </el-table-column>
      <el-table-column v-if="kind === 'department'" :label="t('system.iam.organization.membershipType')" width="150">
        <template #default="{ row }">{{ t(`system.iam.organization.${row.membership_type}`) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.iam.organization.relationRole')" width="140">
        <template #default="{ row }">{{ t(`system.iam.organization.${relationRole(row)}`) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="110">
        <template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'">{{ statusLabel(row.status) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="180" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.status === 'active' && canUpdate" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="row.status === 'active' && canClose" link type="danger" :icon="CircleClose" @click="closeMembership(row)">{{ t('system.iam.common.close') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="formVisible" append-to-body :title="formMode === 'create' ? t('system.iam.organization.addMember') : t('system.iam.common.edit')" width="500px" :close-on-click-modal="false">
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('system.iam.organization.versionConflict')" />
      <el-form label-position="top">
        <el-form-item v-if="formMode === 'create'" :label="t('system.iam.organization.tenantMembership')">
          <el-select
            v-model="form.tenantMembershipId"
            filterable
            remote
            reserve-keyword
            :remote-method="loadCandidates"
            :loading="candidateLoading"
            style="width: 100%"
          >
            <el-option v-for="candidate in candidates" :key="candidate.id" :label="`${candidate.display_name} · ${candidate.username || candidate.principal_id}`" :value="candidate.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="kind === 'department'" :label="t('system.iam.organization.membershipType')">
          <el-select v-model="form.membershipType" style="width: 100%">
            <el-option v-for="item in ['primary', 'additional']" :key="item" :label="t(`system.iam.organization.${item}`)" :value="item" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('system.iam.organization.relationRole')">
          <el-select v-model="form.relationRole" style="width: 100%">
            <el-option v-for="item in roleOptions" :key="item" :label="t(`system.iam.organization.${item}`)" :value="item" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!formValid" @click="saveMembership">{{ t('system.iam.common.save') }}</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  kind: { type: String, required: true },
  organization: { type: Object, default: null }
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const authStore = useAuthStore()
const dialogVisible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const api = computed(() => props.kind === 'department' ? iamAPI.departments : iamAPI.projectGroups)
const permissionPrefix = computed(() => props.kind === 'department' ? 'iam.department_membership' : 'iam.project_group_membership')
const canCreate = computed(() => authStore.hasPermission(`${permissionPrefix.value}.create`))
const canUpdate = computed(() => authStore.hasPermission(`${permissionPrefix.value}.update`))
const canClose = computed(() => authStore.hasPermission(`${permissionPrefix.value}.close`))
const roleOptions = computed(() => props.kind === 'department' ? ['member', 'leader'] : ['member', 'leader', 'coordinator'])
const rows = ref([])
const candidates = ref([])
const loading = ref(false)
const candidateLoading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const status = ref('active')
const formVisible = ref(false)
const formMode = ref('create')
const editingRow = ref(null)
const versionConflict = ref(false)
const form = reactive({ tenantMembershipId: '', membershipType: 'additional', relationRole: 'member' })
const formValid = computed(() => (formMode.value === 'edit' || Boolean(form.tenantMembershipId)) && Boolean(form.relationRole) && (props.kind !== 'department' || Boolean(form.membershipType)))

function statusLabel(value) { return t(`system.iam.status.${value}`) }
function relationRole(row) { return props.kind === 'department' ? row.department_role : row.project_group_role }
function reload() { page.value = 1; return load() }
async function load() {
  if (!props.organization?.id) return
  loading.value = true
  try {
    const result = await api.value.listMemberships(props.organization.id, { page: page.value, page_size: pageSize.value, status: status.value || undefined })
    rows.value = result.data || []; total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
async function loadCandidates(search = '') {
  candidateLoading.value = true
  try {
    const result = await iamAPI.memberships.list({ page: 1, page_size: 100, status: 'active', search: search.trim() || undefined })
    candidates.value = result.data || []
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { candidateLoading.value = false }
}
async function openCreate() {
  formMode.value = 'create'; editingRow.value = null; versionConflict.value = false
  Object.assign(form, { tenantMembershipId: '', membershipType: 'additional', relationRole: 'member' })
  await loadCandidates(); formVisible.value = true
}
function openEdit(row) {
  formMode.value = 'edit'; editingRow.value = row; versionConflict.value = false
  Object.assign(form, { tenantMembershipId: row.tenant_membership_id, membershipType: row.membership_type || 'additional', relationRole: relationRole(row) })
  formVisible.value = true
}
async function saveMembership() {
  submitting.value = true; versionConflict.value = false
  try {
    if (formMode.value === 'create') {
      const payload = { tenant_membership_id: form.tenantMembershipId, relation_role: form.relationRole }
      if (props.kind === 'department') payload.membership_type = form.membershipType
      await api.value.createMembership(props.organization.id, payload)
    } else {
      const payload = { relation_role: form.relationRole, version: editingRow.value.version }
      if (props.kind === 'department') payload.membership_type = form.membershipType
      await api.value.updateMembership(props.organization.id, editingRow.value.id, payload)
    }
    ElMessage.success(t('system.iam.common.saved')); formVisible.value = false; await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'resource_version_conflict') versionConflict.value = true
    ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally { submitting.value = false }
}
async function closeMembership(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t('system.iam.organization.closeMembership'), {
      inputValidator: text => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await api.value.closeMembership(props.organization.id, row.id, row.version, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}

watch(() => [props.modelValue, props.organization?.id], ([visible]) => { if (visible) reload() })
</script>
