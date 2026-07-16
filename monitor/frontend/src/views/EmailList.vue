<template>
  <div class="email-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <div>
            <span class="page-title">{{ t('monitor.email.title') }}</span>
            <div class="page-description">{{ t('monitor.email.description') }}</div>
          </div>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('monitor.email.create') }}
          </el-button>
        </div>
      </template>

      <el-table v-loading="loadingDestinations" :data="destinations" stripe>
        <el-table-column prop="name" :label="t('monitor.email.name')" min-width="150" />
        <el-table-column :label="t('monitor.email.recipients')" min-width="260">
          <template #default="{ row }">
            <div class="recipient-tags">
              <el-tag v-for="recipient in row.recipients" :key="recipient" size="small" type="info">
                {{ recipient }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.event_types')" min-width="250">
          <template #default="{ row }">
            <div class="event-tags">
              <el-tag v-for="eventType in row.event_types" :key="eventType" size="small" type="info">
                {{ eventTypeText(eventType) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.enabled')" width="100">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="value => toggleDestination(row, value)" />
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.updated_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.actions')" width="230" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="openEditDialog(row)">{{ t('monitor.email.edit') }}</el-button>
            <el-button text type="primary" :loading="testingDestinationId === row.id" @click="testDestination(row)">
              {{ t('monitor.email.test') }}
            </el-button>
            <el-button text type="danger" :loading="deletingDestinationId === row.id" @click="deleteDestination(row)">
              {{ t('monitor.email.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loadingDestinations && destinations.length === 0" :description="t('monitor.email.empty')" />
    </el-card>

    <el-card class="delivery-card">
      <template #header><span class="page-title">{{ t('monitor.email.delivery.title') }}</span></template>
      <el-form :inline="true" class="filter-form">
        <el-form-item :label="t('monitor.email.delivery.destination')">
          <el-select v-model="deliveryFilters.destination_id" clearable class="destination-filter">
            <el-option v-for="destination in destinations" :key="destination.id" :label="destination.name" :value="destination.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.email.delivery.status')">
          <el-select v-model="deliveryFilters.status" clearable class="status-filter">
            <el-option v-for="status in deliveryStatuses" :key="status" :label="deliveryStatusText(status)" :value="status" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.email.delivery.event_type')">
          <el-select v-model="deliveryFilters.event_type" clearable class="event-filter">
            <el-option v-for="eventType in NOTIFICATION_EVENT_TYPES" :key="eventType" :label="eventTypeText(eventType)" :value="eventType" />
          </el-select>
        </el-form-item>
        <el-button type="primary" @click="searchDeliveries">{{ t('monitor.execution.filter.search') }}</el-button>
      </el-form>

      <el-table v-loading="loadingDeliveries" :data="deliveries" stripe>
        <el-table-column :label="t('monitor.email.delivery.delivery_id')" min-width="180">
          <template #default="{ row }">
            <el-tooltip :content="row.delivery_id" placement="top">
              <span class="delivery-id">{{ row.delivery_id }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="destination_name" :label="t('monitor.email.delivery.destination')" min-width="140" />
        <el-table-column :label="t('monitor.email.delivery.recipients')" min-width="220">
          <template #default="{ row }">
            <el-tooltip :content="row.recipients.join(', ')" placement="top">
              <span class="ellipsis">{{ row.recipients.join(', ') }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.delivery.subject')" min-width="220">
          <template #default="{ row }">
            <el-tooltip :content="row.subject" placement="top">
              <span class="ellipsis">{{ row.subject }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.delivery.event_type')" width="120">
          <template #default="{ row }">{{ eventTypeText(row.event_type) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.delivery.status')" width="120">
          <template #default="{ row }">
            <el-tag :type="notificationDeliveryTagType(row.status)" size="small">{{ deliveryStatusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="attempt_count" :label="t('monitor.email.delivery.attempts')" width="90" />
        <el-table-column prop="manual_retry_count" :label="t('monitor.email.delivery.manual_retries')" width="100" />
        <el-table-column :label="t('monitor.email.delivery.next_attempt_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.next_attempt_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.delivery.last_error')" min-width="220">
          <template #default="{ row }">
            <el-tooltip v-if="row.last_error" :content="row.last_error" placement="top">
              <span class="ellipsis">{{ row.last_error }}</span>
            </el-tooltip>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.delivery.created_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.email.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="canRetryNotificationDelivery(row)"
              text
              type="primary"
              :loading="retryingDeliveryId === row.delivery_id"
              @click="retryDelivery(row)"
            >
              {{ t('monitor.email.delivery.retry') }}
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

    <el-dialog v-model="dialogVisible" :title="editingDestination ? t('monitor.email.edit') : t('monitor.email.create')" width="620px">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="120px">
        <el-form-item :label="t('monitor.email.name')" prop="name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item :label="t('monitor.email.recipients')" prop="recipients">
          <el-select
            v-model="form.recipients"
            multiple
            filterable
            allow-create
            default-first-option
            class="recipient-input"
            :placeholder="t('monitor.email.recipients_placeholder')"
          />
          <div class="form-help">{{ t('monitor.email.recipients_help') }}</div>
        </el-form-item>
        <el-form-item :label="t('monitor.email.event_types')" prop="event_types">
          <el-checkbox-group v-model="form.event_types">
            <el-checkbox v-for="eventType in NOTIFICATION_EVENT_TYPES" :key="eventType" :value="eventType">
              {{ eventTypeText(eventType) }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item :label="t('monitor.email.enabled')">
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
  createEmailDestination,
  deleteEmailDestination,
  listEmailDeliveries,
  listEmailDestinations,
  retryEmailDelivery,
  testEmailDestination,
  updateEmailDestination
} from '@/api/monitor'
import {
  buildEmailDestinationPayload,
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
const form = reactive({ name: '', recipients: [], enabled: true, event_types: [...NOTIFICATION_EVENT_TYPES] })
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

const formRules = computed(() => ({
  name: [{ required: true, message: t('monitor.email.validation.name'), trigger: 'blur' }],
  recipients: [{
    validator: (_rule, value, callback) => {
      const normalized = value.map(item => item.trim()).filter(Boolean)
      if (normalized.length === 0 || normalized.length > 50 || normalized.some(item => !emailPattern.test(item))) {
        callback(new Error(t('monitor.email.validation.recipients')))
      } else callback()
    }, trigger: 'change'
  }],
  event_types: [{ type: 'array', required: true, min: 1, message: t('monitor.email.validation.event_types'), trigger: 'change' }]
}))

async function loadDestinations() {
  loadingDestinations.value = true
  try { destinations.value = await listEmailDestinations() || [] }
  catch (error) { console.error(error) }
  finally { loadingDestinations.value = false }
}

async function loadDeliveries() {
  loadingDeliveries.value = true
  try {
    const response = await listEmailDeliveries({
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
  Object.assign(form, { name: '', recipients: [], enabled: true, event_types: [...NOTIFICATION_EVENT_TYPES] })
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
    recipients: [...destination.recipients],
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
    const payload = buildEmailDestinationPayload(form)
    if (editingDestination.value) await updateEmailDestination(editingDestination.value.id, payload)
    else await createEmailDestination(payload)
    ElMessage.success(t('monitor.email.saved'))
    dialogVisible.value = false
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.email.operation_failed'))
  } finally { saving.value = false }
}

async function toggleDestination(destination, enabled) {
  try {
    await updateEmailDestination(destination.id, { enabled })
    ElMessage.success(enabled ? t('monitor.email.enabled_success') : t('monitor.email.disabled_success'))
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.email.operation_failed'))
  }
}

async function testDestination(destination) {
  testingDestinationId.value = destination.id
  try {
    const result = await testEmailDestination(destination.id)
    ElMessage.success(t('monitor.email.test_success', { count: result.recipients }))
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.email.operation_failed'))
  } finally { testingDestinationId.value = null }
}

async function deleteDestination(destination) {
  const confirmed = await ElMessageBox.confirm(
    t('monitor.email.delete_confirm', { name: destination.name }),
    t('monitor.email.delete_title'),
    {
      confirmButtonText: t('monitor.email.delete'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
      confirmButtonClass: 'el-button--danger'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  deletingDestinationId.value = destination.id
  try {
    const result = await deleteEmailDestination(destination.id)
    ElMessage.success(result.message || t('monitor.email.deleted'))
    if (deliveryFilters.destination_id === destination.id) deliveryFilters.destination_id = ''
    await Promise.all([loadDestinations(), loadDeliveries()])
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.email.operation_failed'))
  } finally { deletingDestinationId.value = null }
}

async function retryDelivery(delivery) {
  const confirmed = await ElMessageBox.confirm(
    t('monitor.email.delivery.retry_confirm'),
    t('monitor.email.delivery.retry_title'),
    {
      confirmButtonText: t('monitor.email.delivery.retry'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  retryingDeliveryId.value = delivery.delivery_id
  try {
    await retryEmailDelivery(delivery.delivery_id)
    ElMessage.success(t('monitor.email.delivery.retry_success'))
    await loadDeliveries()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.email.operation_failed'))
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
.email-list { padding: 20px; background: var(--addp-bg-secondary); }
.delivery-card { margin-top: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.page-title { color: var(--addp-text-primary); font-weight: 500; font-size: 16px; }
.page-description, .form-help { color: var(--addp-text-tertiary); font-size: 12px; margin-top: 6px; line-height: 1.5; }
.recipient-tags, .event-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.delivery-id, .ellipsis { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.delivery-id { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
.filter-form { margin-bottom: 16px; }
.destination-filter { width: 180px; }
.status-filter, .event-filter { width: 150px; }
.recipient-input { width: 100%; }
.pagination { margin-top: 20px; justify-content: flex-end; }
</style>
