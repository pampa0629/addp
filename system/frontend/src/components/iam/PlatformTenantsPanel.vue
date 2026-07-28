<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input
          v-model="filters.search"
          :placeholder="t('system.iam.common.search')"
          clearable
          :prefix-icon="Search"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload">
          <el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('platform.tenant.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('system.iam.tenants.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column prop="code" :label="t('system.iam.common.code')" min-width="150" />
      <el-table-column prop="name" :label="t('system.iam.common.name')" min-width="180" />
      <el-table-column prop="description" :label="t('system.iam.common.description')" min-width="220" show-overflow-tooltip />
      <el-table-column :label="t('system.iam.common.status')" width="120">
        <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.tenants.initialization')" width="130">
        <template #default="{ row }">
          <el-tag :type="row.initialized ? 'success' : 'warning'" effect="plain">
            {{ row.initialized ? t('system.iam.tenants.initialized') : t('system.iam.tenants.uninitialized') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.updatedAt')" width="180">
        <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="390" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('platform.tenant.initialize') && !row.initialized && row.status !== 'closed'" link type="primary" :icon="UserFilled" @click="openInitialize(row)">
            {{ t('system.iam.tenants.initialize') }}
          </el-button>
          <el-button v-if="can('platform.tenant.update') && row.status !== 'closed'" link type="primary" :icon="Edit" @click="openEdit(row)">
            {{ t('system.iam.common.edit') }}
          </el-button>
          <el-button v-if="can('platform.tenant.suspend') && row.status === 'active'" link type="warning" :icon="VideoPause" @click="changeStatus(row, 'suspend')">
            {{ t('system.iam.common.suspend') }}
          </el-button>
          <el-button v-if="can('platform.tenant.restore') && row.status === 'suspended'" link type="success" :icon="RefreshLeft" @click="changeStatus(row, 'restore')">
            {{ t('system.iam.common.restore') }}
          </el-button>
          <el-button v-if="can('platform.tenant.close') && row.status !== 'closed'" link type="danger" :icon="CircleClose" @click="changeStatus(row, 'close')">
            {{ t('system.iam.common.close') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="page"
      v-model:page-size="pageSize"
      class="iam-pagination"
      :total="total"
      :page-sizes="[10, 20, 50]"
      layout="total, sizes, prev, pager, next"
      @current-change="load"
      @size-change="reload"
    />

    <el-dialog v-model="dialogVisible" :title="editing ? t('system.iam.tenants.edit') : t('system.iam.tenants.create')" width="560px">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item v-if="!editing" :label="t('system.iam.common.code')" prop="code">
          <el-input v-model="form.code" />
        </el-form-item>
        <el-form-item :label="t('system.iam.common.name')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('system.iam.common.description')"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item v-if="!editing" :label="t('system.iam.tenants.initialAdministrator')" prop="initialAdministratorPrincipalId">
          <el-select
            v-model="form.initialAdministratorPrincipalId"
            filterable
            remote
            :remote-method="loadCandidates"
            :loading="candidatesLoading"
            style="width: 100%"
          >
            <el-option v-for="candidate in candidates" :key="candidate.principal_id" :label="candidateLabel(candidate)" :value="candidate.principal_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('system.iam.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="initializeVisible" :title="t('system.iam.tenants.initialize')" width="520px">
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.tenants.tenant')"><el-input :model-value="initializingTenant?.name" disabled /></el-form-item>
        <el-form-item :label="t('system.iam.tenants.initialAdministrator')" required>
          <el-select
            v-model="initialAdministratorPrincipalId"
            filterable
            remote
            :remote-method="loadCandidates"
            :loading="candidatesLoading"
            style="width: 100%"
          >
            <el-option v-for="candidate in candidates" :key="candidate.principal_id" :label="candidateLabel(candidate)" :value="candidate.principal_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="initializeVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!initialAdministratorPrincipalId" @click="initializeTenant">{{ t('system.iam.common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Edit, Plus, Refresh, RefreshLeft, Search, UserFilled, VideoPause } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const statuses = ['active', 'suspended', 'closed']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const dialogVisible = ref(false)
const editing = ref(null)
const formRef = ref()
const form = reactive({ code: '', name: '', description: '', initialAdministratorPrincipalId: '' })
const candidates = ref([])
const candidatesLoading = ref(false)
const initializeVisible = ref(false)
const initializingTenant = ref(null)
const initialAdministratorPrincipalId = ref('')
const rules = computed(() => ({
  code: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  name: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  initialAdministratorPrincipalId: [{ required: !editing.value, message: t('system.iam.validation.required'), trigger: 'change' }]
}))

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function statusType(status) { return ({ active: 'success', suspended: 'warning', closed: 'info' })[status] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }

async function load() {
  loading.value = true
  try {
    const result = await iamAPI.platformTenants.list({ page: page.value, page_size: pageSize.value, ...filters })
    rows.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function reload() { page.value = 1; return load() }
function candidateLabel(candidate) {
  const account = candidate.username || candidate.primary_email || candidate.principal_id
  return `${candidate.display_name} (${account})`
}
async function loadCandidates(search = '') {
  candidatesLoading.value = true
  try {
    candidates.value = await iamAPI.platformTenants.listAdministratorCandidates({ search }) || []
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    candidatesLoading.value = false
  }
}
async function openCreate() {
  editing.value = null
  Object.assign(form, { code: '', name: '', description: '', initialAdministratorPrincipalId: '' })
  dialogVisible.value = true
  await loadCandidates()
}
function openEdit(row) {
  editing.value = row
  Object.assign(form, { code: row.code, name: row.name, description: row.description || '', initialAdministratorPrincipalId: '' })
  dialogVisible.value = true
}
async function openInitialize(row) {
  initializingTenant.value = row
  initialAdministratorPrincipalId.value = ''
  initializeVisible.value = true
  await loadCandidates()
}
async function submit() {
  await formRef.value?.validate()
  submitting.value = true
  try {
    if (editing.value) await iamAPI.platformTenants.update(editing.value.id, { name: form.name, description: form.description })
    else await iamAPI.platformTenants.create({
      code: form.code,
      name: form.name,
      description: form.description,
      initial_administrator_principal_id: form.initialAdministratorPrincipalId
    })
    ElMessage.success(t('system.iam.common.saved'))
    dialogVisible.value = false
    await load()
  } catch (error) {
    if (error !== false) ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally {
    submitting.value = false
  }
}
async function initializeTenant() {
  submitting.value = true
  try {
    await iamAPI.platformTenants.initialize(initializingTenant.value.id, initialAdministratorPrincipalId.value)
    ElMessage.success(t('system.iam.tenants.initializedSuccess'))
    initializeVisible.value = false
    await load()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  } finally {
    submitting.value = false
  }
}
async function changeStatus(row, action) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t(`system.iam.common.${action}`), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'),
      type: action === 'close' ? 'warning' : 'info'
    })
    await iamAPI.platformTenants[action](row.id, value.trim())
    ElMessage.success(t('system.iam.common.updated'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  }
}

onMounted(load)
</script>
