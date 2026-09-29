<template>
  <div>
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.common.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" clearable :placeholder="t('system.iam.common.status')" @change="reload">
          <el-option v-for="item in ['active', 'closed']" :key="item" :label="statusLabel(item)" :value="item" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.project_group.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.organization.projectGroups.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.common.name')" min-width="220"><template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.name }}</strong><div class="iam-code-line"><span>{{ row.code }}</span><el-tooltip :content="t('system.iam.organization.copyCode')"><el-button link type="primary" :icon="CopyDocument" :aria-label="t('system.iam.organization.copyCode')" @click="copyCode(row.code)" /></el-tooltip></div></div></template></el-table-column>
      <el-table-column :label="t('system.iam.common.description')" min-width="220" show-overflow-tooltip prop="description" />
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="280" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.project_group_membership.read')" link type="primary" :icon="User" @click="openMembers(row)">{{ t('system.iam.organization.members') }}</el-button>
          <el-button v-if="row.status !== 'closed' && can('iam.project_group.update')" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="row.status !== 'closed' && can('iam.project_group.close')" link type="danger" :icon="CircleClose" @click="closeGroup(row)">{{ t('system.iam.organization.projectGroups.close') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="formVisible" :title="formMode === 'create' ? t('system.iam.organization.projectGroups.create') : t('system.iam.organization.projectGroups.edit')" width="600px" :close-on-click-modal="false">
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('system.iam.organization.versionConflict')" />
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.common.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('system.iam.common.code')" :required="formMode === 'create'" :error="organizationCodeError">
          <el-input v-model="form.code" :disabled="formMode === 'edit'" />
          <el-text type="info" size="small">{{ t(formMode === 'create' ? 'system.iam.organization.codeCreateHint' : 'system.iam.organization.codeImmutableHint') }}</el-text>
        </el-form-item>
        <el-form-item v-if="formMode === 'edit'" :label="t('system.iam.common.description')"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" :disabled="!formValid" @click="save">{{ t('system.iam.common.save') }}</el-button></template>
    </el-dialog>

  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, CopyDocument, Edit, Plus, Refresh, Search, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { isValidOrganizationCode } from '../../utils/organizationIdentity'

const emit = defineEmits(['open-members'])

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const formVisible = ref(false)
const formMode = ref('create')
const editing = ref(null)
const versionConflict = ref(false)
const codeConflict = ref(false)
const form = reactive({ code: '', name: '', description: '' })
const organizationCodeValid = computed(() => isValidOrganizationCode(form.code))
const organizationCodeError = computed(() => codeConflict.value ? t('system.iam.organization.codeAlreadyExists') : form.code && !organizationCodeValid.value ? t('system.iam.validation.organizationCode') : '')
const formValid = computed(() => organizationCodeValid.value && Boolean(form.name.trim()))
watch(() => form.code, () => { codeConflict.value = false })

function statusLabel(value) { return value === 'active' ? t('system.iam.organization.projectGroups.inUse') : t('system.iam.status.closed') }
function statusType(value) { return value === 'active' ? 'success' : 'info' }
async function load() {
  loading.value = true
  try {
    const result = await iamAPI.projectGroups.list({ page: page.value, page_size: pageSize.value, search: filters.search || undefined, status: filters.status || undefined })
    rows.value = result.data || []; total.value = result.total || 0
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function reload() { page.value = 1; return load() }
function openCreate() {
  formMode.value = 'create'; editing.value = null; versionConflict.value = false
  codeConflict.value = false; Object.assign(form, { code: '', name: '', description: '' }); formVisible.value = true
}
function openEdit(row) {
  formMode.value = 'edit'; editing.value = row; versionConflict.value = false
  Object.assign(form, { code: row.code, name: row.name, description: row.description || '' }); formVisible.value = true
}
function requestPayload() {
  return { code: form.code.trim(), name: form.name.trim() }
}
async function save() {
  submitting.value = true; versionConflict.value = false
  try {
    const payload = requestPayload()
    if (formMode.value === 'create') await iamAPI.projectGroups.create(payload)
    else await iamAPI.projectGroups.update(editing.value.id, { name: form.name.trim(), description: form.description.trim(), version: editing.value.version })
    ElMessage.success(t('system.iam.common.saved')); formVisible.value = false; await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'resource_version_conflict') versionConflict.value = true
    if (error.response?.data?.error_code === 'organization_code_already_exists') codeConflict.value = true
    ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally { submitting.value = false }
}
async function closeGroup(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.organization.projectGroups.closeHint'), t('system.iam.organization.projectGroups.close'), {
      inputValidator: text => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.projectGroups.close(row.id, row.version, value.trim())
    ElMessage.success(t('system.iam.common.updated')); await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed')) }
}
function openMembers(row) { emit('open-members', row) }
async function copyCode(code) {
  try {
    await navigator.clipboard.writeText(code)
    ElMessage.success(t('system.iam.organization.codeCopied'))
  } catch {
    ElMessage.error(t('system.iam.organization.codeCopyFailed'))
  }
}
onMounted(load)
</script>
