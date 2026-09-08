<template>
  <section class="iam-panel">
    <el-alert class="iam-panel-intro" type="info" :closable="false" show-icon>
      <template #title>{{ t('system.iam.serviceAccounts.introTitle') }}</template>
      <p>{{ t('system.iam.serviceAccounts.introDescription') }}</p>
    </el-alert>

    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.serviceAccounts.search')" clearable :prefix-icon="Search" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="filters.status" :placeholder="t('system.iam.common.status')" clearable @change="reload">
          <el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" />
        </el-select>
        <el-button :icon="Refresh" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.service_account.create')" type="primary" :icon="Plus" @click="openCreate">{{ t('system.iam.serviceAccounts.create') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.tabs.serviceAccounts')" min-width="220">
        <template #default="{ row }"><div class="iam-primary-cell"><strong>{{ row.name }}</strong><span>{{ row.description || '-' }}</span></div></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.serviceAccounts.clientId')" min-width="240">
        <template #default="{ row }"><span class="iam-copy-value">{{ row.client_id }}</span><el-button link type="primary" :icon="CopyDocument" @click="copyText(row.client_id, 'clientIdCopied')">{{ t('system.iam.common.copy') }}</el-button></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'warning'">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.common.updatedAt')" width="180"><template #default="{ row }">{{ formatDate(row.updated_at) }}</template></el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="300" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.service_account.update')" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="can('iam.service_credential.update')" link type="primary" :icon="Key" @click="rotateSecret(row)">{{ t('system.iam.serviceAccounts.rotateSecret') }}</el-button>
          <el-button v-if="row.status === 'active' && can('iam.service_account.suspend')" link type="warning" :icon="VideoPause" @click="changeStatus(row, 'suspend')">{{ t('system.iam.common.suspend') }}</el-button>
          <el-button v-if="row.status === 'suspended' && can('iam.service_account.restore')" link type="success" :icon="RefreshLeft" @click="changeStatus(row, 'restore')">{{ t('system.iam.common.restore') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="reload" />

    <el-dialog v-model="formVisible" :title="editing ? t('system.iam.serviceAccounts.edit') : t('system.iam.serviceAccounts.create')" width="560px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('system.iam.common.name')" required><el-input v-model="form.name" maxlength="120" show-word-limit /></el-form-item>
        <el-form-item :label="t('system.iam.common.description')"><el-input v-model="form.description" type="textarea" :rows="3" maxlength="500" show-word-limit /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="submitForm">{{ t('system.iam.common.confirm') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="credentialVisible" :title="t('system.iam.serviceAccounts.credentialTitle')" width="620px" :close-on-click-modal="false" :close-on-press-escape="false" :show-close="false">
      <el-alert type="warning" :closable="false" show-icon :title="t('system.iam.serviceAccounts.secretOnce')" />
      <dl class="iam-credential-list">
        <div><dt>{{ t('system.iam.serviceAccounts.clientId') }}</dt><dd><code>{{ credential.clientId }}</code><el-button link type="primary" :icon="CopyDocument" @click="copyText(credential.clientId, 'clientIdCopied')">{{ t('system.iam.common.copy') }}</el-button></dd></div>
        <div><dt>{{ t('system.iam.serviceAccounts.clientSecret') }}</dt><dd><code>{{ credential.clientSecret }}</code><el-button link type="primary" :icon="CopyDocument" @click="copyText(credential.clientSecret, 'secretCopied')">{{ t('system.iam.common.copy') }}</el-button></dd></div>
      </dl>
      <template #footer><el-button type="primary" @click="credentialVisible = false">{{ t('system.iam.common.done') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CopyDocument, Edit, Key, Plus, Refresh, RefreshLeft, Search, VideoPause } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const statuses = ['active', 'suspended']
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const formVisible = ref(false)
const editing = ref(null)
const form = reactive({ name: '', description: '' })
const credentialVisible = ref(false)
const credential = reactive({ clientId: '', clientSecret: '' })

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }

async function load() {
  loading.value = true
  try {
    const result = await iamAPI.serviceAccounts.list({ page: page.value, page_size: pageSize.value, ...filters })
    rows.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function reload() { page.value = 1; return load() }
function openCreate() { editing.value = null; form.name = ''; form.description = ''; formVisible.value = true }
function openEdit(row) { editing.value = row; form.name = row.name; form.description = row.description || ''; formVisible.value = true }

async function submitForm() {
  if (!form.name.trim()) {
    ElMessage.warning(t('system.iam.validation.required'))
    return
  }
  submitting.value = true
  try {
    if (editing.value) {
      await iamAPI.serviceAccounts.update(editing.value.id, { name: form.name.trim(), description: form.description.trim(), version: editing.value.version })
      ElMessage.success(t('system.iam.common.saved'))
    } else {
      const created = await iamAPI.serviceAccounts.create({ name: form.name.trim(), description: form.description.trim() })
      showCredential(created)
    }
    formVisible.value = false
    await load()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally {
    submitting.value = false
  }
}

async function rotateSecret(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.serviceAccounts.rotateHint'), t('system.iam.serviceAccounts.rotateSecret'), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    const result = await iamAPI.serviceAccounts.rotateSecret(row.id, row.version, value.trim())
    showCredential(result)
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  }
}

async function changeStatus(row, action) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t(`system.iam.common.${action}`), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.serviceAccounts[action](row.id, row.version, value.trim())
    ElMessage.success(t('system.iam.common.updated'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  }
}

function showCredential(result) {
  credential.clientId = result.client_id
  credential.clientSecret = result.client_secret
  credentialVisible.value = true
}

async function copyText(value, messageKey) {
  try {
    await navigator.clipboard.writeText(value)
    ElMessage.success(t(`system.iam.serviceAccounts.${messageKey}`))
  } catch {
    ElMessage.error(t('system.iam.serviceAccounts.copyFailed'))
  }
}

onMounted(load)
</script>

<style scoped>
.iam-panel-intro { margin-bottom: 16px; }
.iam-panel-intro p { margin: 4px 0 0; line-height: 1.5; }
.iam-copy-value { margin-right: 8px; font-family: var(--el-font-family-monospace, monospace); font-size: 12px; }
.iam-credential-list { display: grid; gap: 16px; margin: 18px 0 0; }
.iam-credential-list > div { display: grid; gap: 6px; }
.iam-credential-list dt { color: var(--addp-text-secondary); font-size: 13px; }
.iam-credential-list dd { display: flex; align-items: center; gap: 8px; min-width: 0; margin: 0; }
.iam-credential-list code { flex: 1; overflow-wrap: anywhere; padding: 10px 12px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
</style>
