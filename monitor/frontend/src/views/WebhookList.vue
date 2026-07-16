<template>
  <div class="webhook-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <div>
            <span class="page-title">{{ t('monitor.webhook.title') }}</span>
            <div class="page-description">{{ t('monitor.webhook.description') }}</div>
          </div>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('monitor.webhook.create') }}
          </el-button>
        </div>
      </template>

      <el-table v-loading="loadingDestinations" :data="destinations" stripe>
        <el-table-column prop="name" :label="t('monitor.webhook.name')" min-width="150" />
        <el-table-column :label="t('monitor.webhook.url')" min-width="280">
          <template #default="{ row }">
            <el-tooltip :content="row.url" placement="top">
              <span class="endpoint-url">{{ row.url }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.event_types')" min-width="250">
          <template #default="{ row }">
            <div class="event-tags">
              <el-tag v-for="eventType in row.event_types" :key="eventType" size="small" type="info">
                {{ eventTypeText(eventType) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.secret')" width="120">
          <template #default="{ row }">
            <el-tag :type="row.secret_configured ? 'success' : 'danger'" size="small">
              {{ row.secret_configured ? t('monitor.webhook.secret_configured') : t('monitor.webhook.secret_missing') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.enabled')" width="100">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="value => toggleDestination(row, value)" />
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.updated_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.actions')" width="230" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="openEditDialog(row)">{{ t('monitor.webhook.edit') }}</el-button>
            <el-button text type="primary" :loading="testingDestinationId === row.id" @click="testDestination(row)">
              {{ t('monitor.webhook.test') }}
            </el-button>
            <el-button text type="danger" :loading="deletingDestinationId === row.id" @click="deleteDestination(row)">
              {{ t('monitor.webhook.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loadingDestinations && destinations.length === 0" :description="t('monitor.webhook.empty')" />
    </el-card>

    <el-card class="delivery-card">
      <template #header><span class="page-title">{{ t('monitor.webhook.delivery.title') }}</span></template>
      <el-form :inline="true" class="filter-form">
        <el-form-item :label="t('monitor.webhook.delivery.destination')">
          <el-select v-model="deliveryFilters.destination_id" clearable class="destination-filter">
            <el-option v-for="destination in destinations" :key="destination.id" :label="destination.name" :value="destination.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.delivery.status')">
          <el-select v-model="deliveryFilters.status" clearable class="status-filter">
            <el-option v-for="status in deliveryStatuses" :key="status" :label="deliveryStatusText(status)" :value="status" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.delivery.event_type')">
          <el-select v-model="deliveryFilters.event_type" clearable class="event-filter">
            <el-option v-for="eventType in NOTIFICATION_EVENT_TYPES" :key="eventType" :label="eventTypeText(eventType)" :value="eventType" />
          </el-select>
        </el-form-item>
        <el-button type="primary" @click="searchDeliveries">{{ t('monitor.execution.filter.search') }}</el-button>
      </el-form>

      <el-table v-loading="loadingDeliveries" :data="deliveries" stripe>
        <el-table-column :label="t('monitor.webhook.delivery.delivery_id')" min-width="180">
          <template #default="{ row }">
            <el-tooltip :content="row.delivery_id" placement="top">
              <span class="delivery-id">{{ row.delivery_id }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="destination_name" :label="t('monitor.webhook.delivery.destination')" min-width="140" />
        <el-table-column :label="t('monitor.webhook.delivery.event_type')" width="120">
          <template #default="{ row }">{{ eventTypeText(row.event_type) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.delivery.status')" width="120">
          <template #default="{ row }">
            <el-tag :type="notificationDeliveryTagType(row.status)" size="small">{{ deliveryStatusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="attempt_count" :label="t('monitor.webhook.delivery.attempts')" width="90" />
        <el-table-column prop="manual_retry_count" :label="t('monitor.webhook.delivery.manual_retries')" width="100" />
        <el-table-column :label="t('monitor.webhook.delivery.http_status')" width="100">
          <template #default="{ row }">{{ row.last_http_status || '-' }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.delivery.next_attempt_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.next_attempt_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.delivery.last_error')" min-width="220">
          <template #default="{ row }">
            <el-tooltip v-if="row.last_error" :content="row.last_error" placement="top">
              <span class="last-error">{{ row.last_error }}</span>
            </el-tooltip>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.delivery.created_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.webhook.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="canRetryNotificationDelivery(row)"
              text
              type="primary"
              :loading="retryingDeliveryId === row.delivery_id"
              @click="retryDelivery(row)"
            >
              {{ t('monitor.webhook.delivery.retry') }}
            </el-button>
            <span v-else>-</span>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="deliveryPagination.page"
        v-model:page-size="deliveryPagination.pageSize"
        :total="deliveryPagination.total"
        layout="total, prev, pager, next"
        class="pagination"
        @current-change="loadDeliveries"
      />
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editingDestination ? t('monitor.webhook.edit') : t('monitor.webhook.create')" width="620px">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="120px">
        <el-form-item :label="t('monitor.webhook.name')" prop="name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.url')" prop="url">
          <el-input v-model="form.url" :placeholder="t('monitor.webhook.url_placeholder')" />
          <div class="form-help">{{ t('monitor.webhook.url_help') }}</div>
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.secret')" prop="secret">
          <el-input v-model="form.secret" type="password" show-password autocomplete="new-password" :placeholder="secretPlaceholder" />
          <div class="form-help">{{ t('monitor.webhook.secret_help') }}</div>
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.event_types')" prop="event_types">
          <el-checkbox-group v-model="form.event_types">
            <el-checkbox v-for="eventType in NOTIFICATION_EVENT_TYPES" :key="eventType" :value="eventType">
              {{ eventTypeText(eventType) }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item :label="t('monitor.webhook.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveDestination">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createWebhookDestination,
  deleteWebhookDestination,
  listWebhookDeliveries,
  listWebhookDestinations,
  retryWebhookDelivery,
  testWebhookDestination,
  updateWebhookDestination
} from '@/api/monitor'
import {
  buildWebhookDestinationPayload,
  canRetryNotificationDelivery,
  NOTIFICATION_EVENT_TYPES,
  notificationDeliveryTagType
} from '@/utils/notification'

const { t } = useI18n()
const destinations = ref([])
const deliveries = ref([])
const loadingDestinations = ref(false)
const loadingDeliveries = ref(false)
const saving = ref(false)
const testingDestinationId = ref(null)
const deletingDestinationId = ref(null)
const retryingDeliveryId = ref(null)
const dialogVisible = ref(false)
const editingDestination = ref(null)
const formRef = ref(null)
let refreshTimer = null

const deliveryStatuses = ['pending', 'delivering', 'delivered', 'dead', 'suppressed', 'cancelled']
const deliveryFilters = reactive({ destination_id: '', status: '', event_type: '' })
const deliveryPagination = reactive({ page: 1, pageSize: 20, total: 0 })
const form = reactive({ name: '', url: '', secret: '', enabled: true, event_types: [...NOTIFICATION_EVENT_TYPES] })

const secretPlaceholder = computed(() => editingDestination.value
  ? t('monitor.webhook.secret_keep_placeholder')
  : t('monitor.webhook.secret_placeholder'))

const formRules = computed(() => ({
  name: [{ required: true, message: t('monitor.webhook.validation.name'), trigger: 'blur' }],
  url: [{ required: true, message: t('monitor.webhook.validation.url'), trigger: 'blur' }],
  secret: [{
    validator: (_rule, value, callback) => {
      if (!editingDestination.value && (!value || value.trim().length < 16)) callback(new Error(t('monitor.webhook.validation.secret')))
      else if (value && value.trim().length < 16) callback(new Error(t('monitor.webhook.validation.secret')))
      else callback()
    }, trigger: 'blur'
  }],
  event_types: [{ type: 'array', required: true, min: 1, message: t('monitor.webhook.validation.event_types'), trigger: 'change' }]
}))

async function loadDestinations() {
  loadingDestinations.value = true
  try { destinations.value = await listWebhookDestinations() || [] }
  catch (error) { console.error(error) }
  finally { loadingDestinations.value = false }
}

async function loadDeliveries() {
  loadingDeliveries.value = true
  try {
    const response = await listWebhookDeliveries({
      ...deliveryFilters,
      page: deliveryPagination.page,
      page_size: deliveryPagination.pageSize
    })
    deliveries.value = response.data || []
    deliveryPagination.total = response.total || 0
  } catch (error) { console.error(error) }
  finally { loadingDeliveries.value = false }
}

function searchDeliveries() { deliveryPagination.page = 1; loadDeliveries() }

function resetForm() {
  Object.assign(form, { name: '', url: '', secret: '', enabled: true, event_types: [...NOTIFICATION_EVENT_TYPES] })
  formRef.value?.clearValidate()
}

function openCreateDialog() {
  editingDestination.value = null
  resetForm()
  dialogVisible.value = true
}

function openEditDialog(destination) {
  editingDestination.value = destination
  Object.assign(form, {
    name: destination.name,
    url: destination.url,
    secret: '',
    enabled: destination.enabled,
    event_types: [...destination.event_types]
  })
  formRef.value?.clearValidate()
  dialogVisible.value = true
}

async function saveDestination() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    const payload = buildWebhookDestinationPayload(form, Boolean(editingDestination.value))
    if (editingDestination.value) await updateWebhookDestination(editingDestination.value.id, payload)
    else await createWebhookDestination(payload)
    ElMessage.success(t('monitor.webhook.saved'))
    dialogVisible.value = false
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.webhook.operation_failed'))
  } finally { saving.value = false }
}

async function toggleDestination(destination, enabled) {
  try {
    await updateWebhookDestination(destination.id, { enabled })
    ElMessage.success(enabled ? t('monitor.webhook.enabled_success') : t('monitor.webhook.disabled_success'))
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.webhook.operation_failed'))
  }
}

async function testDestination(destination) {
  testingDestinationId.value = destination.id
  try {
    const result = await testWebhookDestination(destination.id)
    ElMessage.success(t('monitor.webhook.test_success', { status: result.http_status }))
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.webhook.operation_failed'))
  } finally { testingDestinationId.value = null }
}

async function deleteDestination(destination) {
  const confirmed = await ElMessageBox.confirm(
    t('monitor.webhook.delete_confirm', { name: destination.name }),
    t('monitor.webhook.delete_title'),
    {
      confirmButtonText: t('monitor.webhook.delete'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
      confirmButtonClass: 'el-button--danger'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  deletingDestinationId.value = destination.id
  try {
    const result = await deleteWebhookDestination(destination.id)
    ElMessage.success(result.message || t('monitor.webhook.deleted'))
    if (deliveryFilters.destination_id === destination.id) deliveryFilters.destination_id = ''
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.webhook.operation_failed'))
  } finally { deletingDestinationId.value = null }
}

async function retryDelivery(delivery) {
  const confirmed = await ElMessageBox.confirm(
    t('monitor.webhook.delivery.retry_confirm'),
    t('monitor.webhook.delivery.retry_title'),
    {
      confirmButtonText: t('monitor.webhook.delivery.retry'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  retryingDeliveryId.value = delivery.delivery_id
  try {
    await retryWebhookDelivery(delivery.delivery_id)
    ElMessage.success(t('monitor.webhook.delivery.retry_success'))
    await loadDeliveries()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.webhook.operation_failed'))
  } finally { retryingDeliveryId.value = null }
}

function eventTypeText(eventType) { return t(`monitor.notification.event_type_values.${eventType}`) }
function deliveryStatusText(status) { return t(`monitor.notification.status_values.${status}`) }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }

onMounted(async () => {
  await Promise.all([loadDestinations(), loadDeliveries()])
  refreshTimer = window.setInterval(loadDeliveries, 10000)
})
onBeforeUnmount(() => { if (refreshTimer) window.clearInterval(refreshTimer) })
</script>

<style scoped>
.webhook-list { padding: 20px; background: var(--addp-bg-secondary); }
.delivery-card { margin-top: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.page-title { color: var(--addp-text-primary); font-weight: 500; font-size: 16px; }
.page-description, .form-help { color: var(--addp-text-tertiary); font-size: 12px; margin-top: 6px; line-height: 1.5; }
.event-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.endpoint-url, .delivery-id, .last-error { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.delivery-id { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
.filter-form { margin-bottom: 16px; }
.destination-filter { width: 180px; }
.status-filter, .event-filter { width: 150px; }
.pagination { margin-top: 20px; justify-content: flex-end; }
</style>
