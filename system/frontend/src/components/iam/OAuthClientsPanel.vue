<template>
  <section class="iam-panel">
    <StatusAnnouncer :message="announcement" />
    <el-alert
      class="oauth-client-intro"
      type="info"
      :closable="false"
      show-icon
      :title="t('system.iam.oauthClients.introTitle')"
      :description="t('system.iam.oauthClients.introDescription')"
    />

    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input
          v-model="filters.search"
          :placeholder="t('system.iam.oauthClients.search')"
          clearable
          :prefix-icon="Search"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="filters.status" clearable :placeholder="t('system.iam.common.status')" @change="reload">
          <el-option v-for="item in ['active', 'disabled']" :key="item" :label="statusLabel(item)" :value="item" />
        </el-select>
        <el-button :icon="Refresh" :loading="loading" @click="reload">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.oauth_client.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('system.iam.oauthClients.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.oauthClients.client')" min-width="260">
        <template #default="{ row }">
          <div class="iam-primary-cell">
            <strong>{{ row.display_name }}</strong>
            <span class="oauth-client-id">{{ row.client_id }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.oauthClients.redirectUris')" min-width="340">
        <template #default="{ row }">
          <ul class="oauth-uri-list">
            <li v-for="uri in row.redirect_uris" :key="uri">{{ uri }}</li>
          </ul>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.oauthClients.protocol')" width="210">
        <template #default>
          <span>{{ t('system.iam.oauthClients.protocolSummary') }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.status')" width="120">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.updatedAt')" width="180">
        <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="260" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :icon="CopyDocument" @click="copyClientID(row.client_id)">
            {{ t('system.iam.oauthClients.copyClientId') }}
          </el-button>
          <el-button v-if="can('iam.oauth_client.update')" link type="primary" :icon="Edit" @click="openEdit(row)">
            {{ t('system.iam.common.edit') }}
          </el-button>
          <el-button
            v-if="row.status === 'active' && can('iam.oauth_client.suspend')"
            link
            type="danger"
            :icon="CircleClose"
            @click="changeStatus(row, 'suspend')"
          >
            {{ t('system.iam.oauthClients.suspend') }}
          </el-button>
          <el-button
            v-if="row.status === 'disabled' && can('iam.oauth_client.restore')"
            link
            type="success"
            :icon="RefreshLeft"
            @click="changeStatus(row, 'restore')"
          >
            {{ t('system.iam.common.restore') }}
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

    <el-dialog
      v-model="formVisible"
      class="addp-dialog"
      :title="formMode === 'create' ? t('system.iam.oauthClients.create') : t('system.iam.oauthClients.edit')"
      width="min(680px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      @opened="focusPrimaryInput"
    >
      <el-alert v-if="versionConflict" type="warning" :closable="false" show-icon :title="t('system.iam.oauthClients.versionConflict')" />
      <el-form label-position="top" class="oauth-client-form">
        <el-form-item :label="t('system.iam.oauthClients.displayName')" required>
          <el-input ref="displayNameInput" v-model="form.displayName" maxlength="120" show-word-limit />
        </el-form-item>
        <el-form-item :label="t('system.iam.oauthClients.redirectUris')" required :error="redirectURIError">
          <div class="oauth-uri-editor">
            <div v-for="(_, index) in form.redirectUris" :key="index" class="oauth-uri-editor__row">
              <el-input v-model="form.redirectUris[index]" :placeholder="t('system.iam.oauthClients.redirectUriPlaceholder')" />
              <el-button
                :icon="Delete"
                circle
                :disabled="form.redirectUris.length === 1"
                :aria-label="t('system.iam.oauthClients.removeRedirectUri')"
                @click="removeRedirectURI(index)"
              />
            </div>
            <el-button :icon="Plus" :disabled="form.redirectUris.length >= 10" @click="addRedirectURI">
              {{ t('system.iam.oauthClients.addRedirectUri') }}
            </el-button>
          </div>
        </el-form-item>
        <el-alert type="info" :closable="false" :title="t('system.iam.oauthClients.redirectUriRule')" />
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!formValid" @click="save">
          {{ t('system.iam.common.save') }}
        </el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, CopyDocument, Delete, Edit, Plus, Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import { StatusAnnouncer } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { validateOAuthRedirectURIs } from '../../utils/oauthClientValidation'

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const rows = ref([])
const loading = ref(false)
const submitting = ref(false)
const announcementState = reactive({ key: '', params: {}, literal: '' })
const announcement = computed(() => announcementState.literal || (announcementState.key ? t(announcementState.key, announcementState.params) : ''))
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({ search: '', status: '' })
const formVisible = ref(false)
const formMode = ref('create')
const editing = ref(null)
const versionConflict = ref(false)
const displayNameInput = ref(null)
const form = reactive({ displayName: '', redirectUris: [''] })

const normalizedRedirectURIs = computed(() => form.redirectUris.map(value => value.trim()))
const redirectURIError = computed(() => {
  if (form.redirectUris.every(value => !value.trim())) return ''
  return validateOAuthRedirectURIs(normalizedRedirectURIs.value) ? '' : t('system.iam.oauthClients.invalidRedirectUris')
})
const formValid = computed(() => {
  const displayName = form.displayName.trim()
  return Boolean(displayName) && [...displayName].length <= 120 && validateOAuthRedirectURIs(normalizedRedirectURIs.value)
})

function statusLabel(value) { return t(`system.iam.status.${value}`) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
function announce(key, params = {}) { Object.assign(announcementState, { key, params, literal: '' }) }
function announceError(error, fallbackKey) {
  const message = error.response?.data?.error
  Object.assign(announcementState, message
    ? { key: '', params: {}, literal: message }
    : { key: fallbackKey, params: {}, literal: '' })
}

async function load() {
  loading.value = true
  announce('system.iam.oauthClients.loading')
  try {
    const result = await iamAPI.oauthClients.list({
      page: page.value,
      page_size: pageSize.value,
      search: filters.search.trim() || undefined,
      status: filters.status || undefined
    })
    rows.value = result.data || []
    total.value = result.total || 0
    announce('system.iam.oauthClients.loaded', { count: rows.value.length })
  } catch (error) {
    announceError(error, 'system.iam.common.loadFailed')
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  return load()
}

function openCreate() {
  formMode.value = 'create'
  editing.value = null
  versionConflict.value = false
  Object.assign(form, { displayName: '', redirectUris: [''] })
  formVisible.value = true
}

function openEdit(row) {
  formMode.value = 'edit'
  editing.value = row
  versionConflict.value = false
  Object.assign(form, { displayName: row.display_name, redirectUris: [...row.redirect_uris] })
  formVisible.value = true
}

async function focusPrimaryInput() {
  await nextTick()
  displayNameInput.value?.focus()
}

function addRedirectURI() {
  if (form.redirectUris.length < 10) form.redirectUris.push('')
}

function removeRedirectURI(index) {
  if (form.redirectUris.length > 1) form.redirectUris.splice(index, 1)
}

async function save() {
  if (!formValid.value) return
  submitting.value = true
  versionConflict.value = false
  try {
    const payload = { display_name: form.displayName.trim(), redirect_uris: normalizedRedirectURIs.value }
    if (formMode.value === 'create') {
      await iamAPI.oauthClients.create(payload)
    } else {
      await iamAPI.oauthClients.update(editing.value.client_id, { ...payload, version: editing.value.version })
    }
    formVisible.value = false
    announce('system.iam.common.saved')
    ElMessage.success(t('system.iam.common.saved'))
    await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'resource_version_conflict') versionConflict.value = true
    announceError(error, 'system.iam.common.saveFailed')
    ElMessage.error(announcement.value)
  } finally {
    submitting.value = false
  }
}

async function changeStatus(row, action) {
  const suspending = action === 'suspend'
  try {
    const { value } = await ElMessageBox.prompt(
      suspending ? t('system.iam.oauthClients.suspendHint') : t('system.iam.oauthClients.restoreHint'),
      suspending ? t('system.iam.oauthClients.suspend') : t('system.iam.common.restore'),
      {
        customClass: 'addp-message-box',
        inputValidator: text => Boolean(text?.trim()) || t('system.iam.validation.required'),
        confirmButtonText: t('system.iam.common.confirm'),
        cancelButtonText: t('system.iam.common.cancel'),
        confirmButtonClass: suspending ? 'el-button--danger' : '',
        type: 'warning'
      }
    )
    await iamAPI.oauthClients[action](row.client_id, row.version, value.trim())
    announce('system.iam.common.updated')
    ElMessage.success(t('system.iam.common.updated'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      announceError(error, 'system.iam.common.updateFailed')
      ElMessage.error(announcement.value)
    }
  }
}

async function copyClientID(clientID) {
  try {
    await navigator.clipboard.writeText(clientID)
    announce('system.iam.oauthClients.clientIdCopied')
    ElMessage.success(announcement.value)
  } catch {
    announce('system.iam.oauthClients.clientIdCopyFailed')
    ElMessage.error(announcement.value)
  }
}

onMounted(load)
</script>

<style scoped>
.oauth-client-intro {
  margin-bottom: 16px;
}

.oauth-client-id,
.oauth-uri-list {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.oauth-uri-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 0;
  margin: 0;
  color: var(--addp-text-secondary);
  font-size: 12px;
  list-style: none;
  overflow-wrap: anywhere;
}

.oauth-client-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.oauth-uri-editor {
  display: flex;
  width: 100%;
  flex-direction: column;
  gap: 8px;
}

.oauth-uri-editor__row {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
