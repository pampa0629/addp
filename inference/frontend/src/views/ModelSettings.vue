<template>
  <main class="settings-page">
    <header class="page-header">
      <div>
        <h1>{{ t('inference.settings.title') }}</h1>
        <el-tag effect="plain">{{ scopeLabel }}</el-tag>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" circle :loading="loading" :title="t('inference.common.refresh')" @click="loadAll" />
        <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">
          {{ createLabel }}
        </el-button>
      </div>
    </header>

    <el-tabs v-model="activeTab" class="resource-tabs">
      <el-tab-pane :label="t('inference.provider.title')" name="providers">
        <el-table v-loading="loading" :data="providers" row-key="id">
          <el-table-column prop="name" :label="t('inference.common.name')" min-width="150" />
          <el-table-column prop="scope_type" :label="t('inference.common.scope')" width="110">
            <template #default="{ row }">{{ t(`inference.scope.${row.scope_type}`) }}</template>
          </el-table-column>
          <el-table-column prop="adapter_type" :label="t('inference.provider.adapter')" min-width="170" />
          <el-table-column prop="endpoint" :label="t('inference.provider.endpoint')" min-width="260" show-overflow-tooltip />
          <el-table-column :label="t('inference.provider.credential')" width="150">
            <template #default="{ row }">
              <el-tag :type="row.credential?.configured ? 'success' : 'info'" effect="plain">
                {{ row.credential?.configured ? t('inference.provider.configured', { version: row.credential.version }) : t('inference.provider.notConfigured') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('inference.common.status')" width="105">
            <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('inference.common.actions')" width="260" fixed="right">
            <template #default="{ row }">
              <el-button v-if="canManageProvider(row) && can('inference.provider.update')" link type="primary" @click="openEditProvider(row)">{{ t('inference.common.edit') }}</el-button>
              <el-button v-if="canManageProvider(row) && can('inference.provider_credential.update')" link type="primary" @click="openCredential(row)">{{ t('inference.provider.setCredential') }}</el-button>
              <el-button v-if="canManageProvider(row) && can('inference.provider_credential.update') && row.credential?.configured" link type="warning" @click="removeCredential(row)">{{ t('inference.provider.deleteCredential') }}</el-button>
              <el-button v-if="canManageProvider(row) && can('inference.provider.delete')" link type="danger" @click="removeProvider(row)">{{ t('inference.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('inference.deployment.title')" name="deployments">
        <el-table v-loading="loading" :data="deployments" row-key="id">
          <el-table-column prop="name" :label="t('inference.common.name')" min-width="150" />
          <el-table-column :label="t('inference.deployment.provider')" min-width="150">
            <template #default="{ row }">{{ providerName(row.provider_connection_id) }}</template>
          </el-table-column>
          <el-table-column prop="upstream_model" :label="t('inference.deployment.upstreamModel')" min-width="180" />
          <el-table-column :label="t('inference.deployment.operations')" min-width="200">
            <template #default="{ row }"><el-tag v-for="item in row.operations" :key="item" class="value-tag" effect="plain">{{ item }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('inference.deployment.modalities')" min-width="130">
            <template #default="{ row }"><el-tag v-for="item in row.modalities" :key="item" class="value-tag" type="info" effect="plain">{{ item }}</el-tag></template>
          </el-table-column>
          <el-table-column prop="dimension" :label="t('inference.deployment.dimension')" width="105" />
          <el-table-column :label="t('inference.common.status')" width="105">
            <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('inference.common.actions')" width="190" fixed="right">
            <template #default="{ row }">
              <el-button v-if="canManageDeployment(row) && can('inference.deployment.execute')" link type="success" @click="probe(row)">{{ t('inference.deployment.probe') }}</el-button>
              <el-button v-if="canManageDeployment(row) && can('inference.deployment.update')" link type="primary" @click="openEditDeployment(row)">{{ t('inference.common.edit') }}</el-button>
              <el-button v-if="canManageDeployment(row) && can('inference.deployment.delete')" link type="danger" @click="removeDeployment(row)">{{ t('inference.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('inference.profile.title')" name="profiles">
        <el-table v-loading="loading" :data="profiles" row-key="id">
          <el-table-column prop="name" :label="t('inference.common.name')" min-width="160" />
          <el-table-column prop="code" :label="t('inference.profile.code')" min-width="150" />
          <el-table-column prop="scope_type" :label="t('inference.common.scope')" width="110">
            <template #default="{ row }">{{ t(`inference.scope.${row.scope_type}`) }}</template>
          </el-table-column>
          <el-table-column :label="t('inference.profile.deployment')" min-width="200">
            <template #default="{ row }">{{ deploymentName(row.model_deployment_id) }}</template>
          </el-table-column>
          <el-table-column prop="version" :label="t('inference.common.version')" width="90" />
          <el-table-column :label="t('inference.common.status')" width="105">
            <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('inference.common.actions')" width="110" fixed="right">
            <template #default="{ row }">
              <el-button v-if="canManageProfile(row) && can('inference.profile.update')" link type="primary" @click="openEditProfile(row)">{{ t('inference.common.edit') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="dialog.visible" :title="dialogTitle" width="min(620px, calc(100vw - 32px))" destroy-on-close>
      <el-form ref="formRef" :model="form" label-position="top">
        <template v-if="dialog.kind === 'provider'">
          <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
          <div class="form-grid">
            <el-form-item :label="t('inference.common.scope')"><el-input :model-value="scopeLabel" disabled /></el-form-item>
            <el-form-item :label="t('inference.provider.adapter')" required>
              <el-select v-model="form.adapter_type"><el-option label="OpenAI Compatible" value="openai_compatible" /><el-option label="DashScope Multimodal" value="dashscope_multimodal" /></el-select>
            </el-form-item>
          </div>
          <el-form-item :label="t('inference.provider.endpoint')" required><el-input v-model="form.endpoint" placeholder="https://example.com/v1" /></el-form-item>
          <template v-if="contextType === 'platform'">
            <el-form-item :label="t('inference.provider.allowAllTenants')"><el-switch v-model="form.allow_all_tenants" /></el-form-item>
            <el-form-item v-if="!form.allow_all_tenants" :label="t('inference.provider.allowedTenantIds')"><el-input v-model="form.allowed_tenant_ids_text" /></el-form-item>
          </template>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else-if="dialog.kind === 'deployment'">
          <el-form-item :label="t('inference.deployment.provider')" required><el-select v-model="form.provider_connection_id" :disabled="dialog.editing"><el-option v-for="provider in manageableProviders" :key="provider.id" :label="provider.name" :value="provider.id" /></el-select></el-form-item>
          <div class="form-grid">
            <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
            <el-form-item :label="t('inference.deployment.upstreamModel')" required><el-input v-model="form.upstream_model" /></el-form-item>
          </div>
          <el-form-item :label="t('inference.deployment.operations')" required><el-checkbox-group v-model="form.operations"><el-checkbox value="chat">chat</el-checkbox><el-checkbox value="embedding">embedding</el-checkbox><el-checkbox value="rerank">rerank</el-checkbox></el-checkbox-group></el-form-item>
          <el-form-item :label="t('inference.deployment.modalities')" required><el-checkbox-group v-model="form.modalities"><el-checkbox value="text">text</el-checkbox><el-checkbox value="image">image</el-checkbox></el-checkbox-group></el-form-item>
          <el-form-item v-if="form.operations.includes('embedding')" :label="t('inference.deployment.dimension')"><el-input-number v-model="form.dimension" :min="0" /></el-form-item>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else-if="dialog.kind === 'profile'">
          <div class="form-grid">
            <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
            <el-form-item :label="t('inference.profile.code')" required><el-input v-model="form.code" :disabled="dialog.editing" /></el-form-item>
          </div>
          <el-form-item :label="t('inference.profile.deployment')" required><el-select v-model="form.model_deployment_id"><el-option v-for="deployment in deployments" :key="deployment.id" :label="deployment.name" :value="deployment.id" /></el-select></el-form-item>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else>
          <el-form-item :label="t('inference.provider.newCredential')" required><el-input v-model="form.credential" type="password" show-password autocomplete="new-password" /></el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">{{ t('inference.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="save">{{ t('inference.common.save') }}</el-button>
      </template>
    </el-dialog>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { deploymentAPI, profileAPI, providerAPI } from '../api/inference'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const activeTab = ref('providers')
const loading = ref(false)
const submitting = ref(false)
const providers = ref([])
const deployments = ref([])
const profiles = ref([])
const formRef = ref()
const form = reactive({})
const dialog = reactive({ visible: false, kind: 'provider', editing: false, id: null })

const contextType = computed(() => authStore.contextType || 'tenant')
const scopeLabel = computed(() => t(`inference.scope.${contextType.value}`))
const manageableProviders = computed(() => providers.value.filter((item) => item.scope_type === contextType.value))
const createPermissions = { providers: 'inference.provider.create', deployments: 'inference.deployment.create', profiles: 'inference.profile.create' }
const createKeys = { providers: 'inference.provider.create', deployments: 'inference.deployment.create', profiles: 'inference.profile.create' }
const canCreate = computed(() => can(createPermissions[activeTab.value]))
const createLabel = computed(() => t(createKeys[activeTab.value]))
const dialogTitle = computed(() => {
  if (dialog.kind === 'credential') return t('inference.provider.credentialDialog')
  return t(`inference.${dialog.kind}.${dialog.editing ? 'edit' : 'create'}`)
})

function can(permission) { return authStore.hasPermission(permission) }
function canManageProvider(row) { return row.scope_type === contextType.value }
function canManageDeployment(row) { return canManageProvider(providers.value.find((item) => item.id === row.provider_connection_id) || {}) }
function canManageProfile(row) { return row.scope_type === contextType.value }
function statusType(status) { return status === 'active' ? 'success' : 'info' }
function providerName(id) { return providers.value.find((item) => item.id === id)?.name || id }
function deploymentName(id) { return deployments.value.find((item) => item.id === id)?.name || id }
function errorMessage(error) { return error?.response?.data?.error || error?.message || t('inference.common.failed') }

async function loadAll() {
  loading.value = true
  try {
    const [providerPage, deploymentPage, profilePage] = await Promise.all([
      providerAPI.list({ page: 1, page_size: 100 }),
      deploymentAPI.list({ page: 1, page_size: 100 }),
      profileAPI.list({ page: 1, page_size: 100 })
    ])
    providers.value = providerPage.data || []
    deployments.value = deploymentPage.data || []
    profiles.value = profilePage.data || []
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function assignForm(value) {
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form, value)
}

function openCreate() {
  const kind = activeTab.value === 'providers' ? 'provider' : activeTab.value === 'deployments' ? 'deployment' : 'profile'
  dialog.kind = kind
  dialog.editing = false
  dialog.id = null
  if (kind === 'provider') assignForm({ name: '', scope_type: contextType.value, adapter_type: 'openai_compatible', endpoint: '', allow_all_tenants: contextType.value === 'platform', allowed_tenant_ids_text: '', status: 'active' })
  if (kind === 'deployment') assignForm({ provider_connection_id: manageableProviders.value[0]?.id || '', name: '', upstream_model: '', operations: ['chat'], modalities: ['text'], dimension: 0, status: 'active' })
  if (kind === 'profile') assignForm({ name: '', code: '', scope_type: contextType.value, model_deployment_id: deployments.value[0]?.id || '', status: 'active' })
  dialog.visible = true
}

function openEditProvider(row) {
  Object.assign(dialog, { kind: 'provider', editing: true, id: row.id, visible: true })
  assignForm({ name: row.name, scope_type: row.scope_type, adapter_type: row.adapter_type, endpoint: row.endpoint, allow_all_tenants: row.allow_all_tenants, allowed_tenant_ids_text: (row.allowed_tenant_ids || []).join(','), status: row.status })
}

function openEditDeployment(row) {
  Object.assign(dialog, { kind: 'deployment', editing: true, id: row.id, visible: true })
  assignForm({ provider_connection_id: row.provider_connection_id, name: row.name, upstream_model: row.upstream_model, operations: [...(row.operations || [])], modalities: [...(row.modalities || [])], dimension: row.dimension || 0, status: row.status })
}

function openEditProfile(row) {
  Object.assign(dialog, { kind: 'profile', editing: true, id: row.id, visible: true })
  assignForm({ name: row.name, code: row.code, scope_type: row.scope_type, model_deployment_id: row.model_deployment_id, status: row.status })
}

function openCredential(row) {
  Object.assign(dialog, { kind: 'credential', editing: true, id: row.id, visible: true })
  assignForm({ credential: '' })
}

function providerPayload() {
  const ids = form.allowed_tenant_ids_text
    ? [...new Set(form.allowed_tenant_ids_text.split(',').map((item) => Number(item.trim())).filter((item) => Number.isInteger(item) && item > 0))]
    : []
  return { name: form.name, scope_type: form.scope_type, adapter_type: form.adapter_type, endpoint: form.endpoint, allow_all_tenants: form.allow_all_tenants, allowed_tenant_ids: ids, status: form.status }
}

async function save() {
  submitting.value = true
  try {
    if (dialog.kind === 'credential') {
      if (!form.credential?.trim()) throw new Error(t('inference.provider.credentialRequired'))
      await providerAPI.setCredential(dialog.id, form.credential)
    } else if (dialog.kind === 'provider') {
      if (dialog.editing) await providerAPI.update(dialog.id, providerPayload())
      else await providerAPI.create(providerPayload())
    } else if (dialog.kind === 'deployment') {
      const payload = { provider_connection_id: form.provider_connection_id, name: form.name, upstream_model: form.upstream_model, operations: form.operations, modalities: form.modalities, dimension: form.operations.includes('embedding') ? form.dimension : 0, status: form.status }
      if (dialog.editing) await deploymentAPI.update(dialog.id, payload)
      else await deploymentAPI.create(payload)
    } else {
      const payload = { name: form.name, code: form.code, scope_type: form.scope_type, model_deployment_id: form.model_deployment_id, status: form.status }
      if (dialog.editing) await profileAPI.update(dialog.id, payload)
      else await profileAPI.create(payload)
    }
    dialog.visible = false
    ElMessage.success(t('inference.common.saved'))
    await loadAll()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    submitting.value = false
  }
}

async function confirmDelete(messageKey, action) {
  try {
    await ElMessageBox.confirm(t(messageKey), t('inference.common.confirmTitle'), { type: 'warning' })
    await action()
    ElMessage.success(t('inference.common.deleted'))
    await loadAll()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error))
  }
}

function removeProvider(row) { return confirmDelete('inference.provider.deleteConfirm', () => providerAPI.remove(row.id)) }
function removeDeployment(row) { return confirmDelete('inference.deployment.deleteConfirm', () => deploymentAPI.remove(row.id)) }
function removeCredential(row) { return confirmDelete('inference.provider.deleteCredentialConfirm', () => providerAPI.deleteCredential(row.id)) }

async function probe(row) {
  try {
    const result = await deploymentAPI.probe(row.id)
    ElMessage.success(t('inference.deployment.probeSuccess', { status: result.status_code }))
  } catch (error) {
    ElMessage.error(errorMessage(error))
  }
}

onMounted(loadAll)
</script>

<style scoped>
.settings-page { box-sizing: border-box; min-height: 100%; padding: 20px 24px; background: var(--addp-bg-primary); }
.page-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.page-header h1 { margin: 0 0 8px; font-size: 22px; font-weight: 600; letter-spacing: 0; }
.header-actions { display: flex; align-items: center; gap: 8px; }
.resource-tabs { width: 100%; }
.value-tag { margin-right: 4px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
:deep(.el-select) { width: 100%; }
@media (max-width: 720px) {
  .settings-page { padding: 16px; }
  .page-header { align-items: flex-start; flex-direction: column; }
  .form-grid { grid-template-columns: 1fr; }
}
</style>
