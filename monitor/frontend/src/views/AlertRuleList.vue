<template>
  <div class="alert-rule-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <div>
            <span class="page-title">{{ t('monitor.alert_rule.title') }}</span>
            <div class="page-description">{{ t('monitor.alert_rule.description') }}</div>
          </div>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('monitor.alert_rule.create') }}
          </el-button>
        </div>
      </template>

      <el-table v-loading="loading" :data="rules" stripe>
        <el-table-column prop="name" :label="t('monitor.alert_rule.name')" min-width="170" />
        <el-table-column :label="t('monitor.alert_rule.target')" min-width="240">
          <template #default="{ row }">
            <div class="target-name">{{ row.source_task_name || row.source_task_id }}</div>
            <div class="target-identity">{{ row.module }} / {{ row.task_type }} / {{ row.source_task_id }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert_rule.rule_type')" min-width="180">
          <template #default="{ row }">
            {{ ruleTypeText(row.rule_type) }}
            <span v-if="row.rule_type === 'consecutive_failures'"> ({{ row.failure_threshold }})</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert.severity')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'" size="small">
              {{ t(`monitor.alert.severity_values.${row.severity}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert_rule.routes')" min-width="180">
          <template #default="{ row }">
            <div v-if="row.routes?.length" class="route-tags">
              <el-tag v-for="route in row.routes" :key="route.id" size="small" type="info">
                {{ routeText(route) }}
              </el-tag>
            </div>
            <span v-else class="muted">{{ t('monitor.alert_rule.no_routes') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert_rule.enabled')" width="90">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="value => toggleRule(row, value)" />
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert_rule.updated_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="openEditDialog(row)">{{ t('monitor.alert_rule.edit') }}</el-button>
            <el-button text type="danger" @click="removeRule(row)">{{ t('monitor.alert_rule.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rules.length === 0" :description="t('monitor.alert_rule.empty')" />
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editingRule ? t('monitor.alert_rule.edit') : t('monitor.alert_rule.create')" width="680px">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="120px">
        <el-form-item :label="t('monitor.alert_rule.name')" prop="name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item :label="t('monitor.alert_rule.target')" prop="target_key">
          <el-select v-model="form.target_key" filterable class="full-width" :placeholder="t('monitor.alert_rule.target_placeholder')">
            <el-option v-for="target in targets" :key="targetKey(target)" :value="targetKey(target)" :label="targetLabel(target)" />
          </el-select>
          <div class="form-help">{{ t('monitor.alert_rule.target_help') }}</div>
        </el-form-item>
        <el-form-item :label="t('monitor.alert_rule.rule_type')" prop="rule_type">
          <el-select v-model="form.rule_type" class="full-width">
            <el-option v-for="ruleType in ALERT_RULE_TYPES" :key="ruleType" :value="ruleType" :label="ruleTypeText(ruleType)" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.rule_type === 'consecutive_failures'" :label="t('monitor.alert_rule.failure_threshold')" prop="failure_threshold">
          <el-input-number v-model="form.failure_threshold" :min="2" :max="20" controls-position="right" />
        </el-form-item>
        <el-form-item :label="t('monitor.alert.severity')" prop="severity">
          <el-radio-group v-model="form.severity">
            <el-radio-button value="warning">{{ t('monitor.alert.severity_values.warning') }}</el-radio-button>
            <el-radio-button value="critical">{{ t('monitor.alert.severity_values.critical') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('monitor.alert_rule.routes')">
          <el-checkbox-group v-model="form.routes" class="route-options">
            <el-checkbox v-for="destination in webhookDestinations" :key="`webhook:${destination.id}`" :value="`webhook:${destination.id}`">
              Webhook / {{ destination.name }}
            </el-checkbox>
            <el-checkbox v-for="destination in emailDestinations" :key="`email:${destination.id}`" :value="`email:${destination.id}`">
              {{ t('monitor.notification.email_tab') }} / {{ destination.name }}
            </el-checkbox>
          </el-checkbox-group>
          <div class="form-help">{{ t('monitor.alert_rule.routes_help') }}</div>
        </el-form-item>
        <el-form-item :label="t('monitor.alert_rule.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveRule">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  createAlertRule,
  deleteAlertRule,
  listAlertRules,
  listAlertRuleTargets,
  listEmailDestinations,
  listWebhookDestinations,
  updateAlertRule
} from '@/api/monitor'
import {
  ALERT_RULE_TYPES,
  alertRuleRouteKey,
  alertRuleTargetKey,
  buildAlertRulePayload
} from '@/utils/alertRule'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const rules = ref([])
const targets = ref([])
const webhookDestinations = ref([])
const emailDestinations = ref([])
const dialogVisible = ref(false)
const editingRule = ref(null)
const formRef = ref(null)
const form = reactive({
  name: '', target_key: '', rule_type: 'last_terminal_failed', failure_threshold: 2,
  severity: 'warning', enabled: true, routes: []
})

const formRules = computed(() => ({
  name: [{ required: true, message: t('monitor.alert_rule.validation.name'), trigger: 'blur' }],
  target_key: [{ required: true, message: t('monitor.alert_rule.validation.target'), trigger: 'change' }],
  rule_type: [{ required: true, message: t('monitor.alert_rule.validation.rule_type'), trigger: 'change' }]
}))

const destinationNames = computed(() => {
  const names = new Map()
  webhookDestinations.value.forEach(item => names.set(`webhook:${item.id}`, `Webhook / ${item.name}`))
  emailDestinations.value.forEach(item => names.set(`email:${item.id}`, `${t('monitor.notification.email_tab')} / ${item.name}`))
  return names
})

function targetKey(target) { return alertRuleTargetKey(target) }
function targetLabel(target) {
  const name = target.source_task_name || target.source_task_id
  return `${name} (${target.module} / ${target.task_type} / ${target.source_task_id})`
}
function ruleTypeText(ruleType) { return ruleType ? t(`monitor.alert_rule.rule_types.${ruleType}`) : '-' }
function routeText(route) { return destinationNames.value.get(alertRuleRouteKey(route)) || `${route.channel} / ${route.destination_id}` }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }

async function loadData() {
  loading.value = true
  try {
    const [ruleData, targetData, webhookData, emailData] = await Promise.all([
      listAlertRules(), listAlertRuleTargets(), listWebhookDestinations(), listEmailDestinations()
    ])
    rules.value = ruleData || []
    targets.value = targetData || []
    webhookDestinations.value = webhookData || []
    emailDestinations.value = emailData || []
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.alert_rule.operation_failed'))
  } finally { loading.value = false }
}

function resetForm() {
  Object.assign(form, {
    name: '', target_key: '', rule_type: 'last_terminal_failed', failure_threshold: 2,
    severity: 'warning', enabled: true, routes: []
  })
  formRef.value?.clearValidate()
}

function openCreateDialog() {
  editingRule.value = null
  resetForm()
  dialogVisible.value = true
}

function openEditDialog(rule) {
  editingRule.value = rule
  Object.assign(form, {
    name: rule.name,
    target_key: alertRuleTargetKey(rule),
    rule_type: rule.rule_type,
    failure_threshold: rule.failure_threshold,
    severity: rule.severity,
    enabled: rule.enabled,
    routes: (rule.routes || []).map(alertRuleRouteKey)
  })
  formRef.value?.clearValidate()
  dialogVisible.value = true
}

async function saveRule() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  const target = targets.value.find(item => targetKey(item) === form.target_key)
  if (!target) return
  saving.value = true
  try {
    const payload = buildAlertRulePayload(form, target)
    if (editingRule.value) await updateAlertRule(editingRule.value.id, payload)
    else await createAlertRule(payload)
    ElMessage.success(t('monitor.alert_rule.saved'))
    dialogVisible.value = false
    await loadData()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.alert_rule.operation_failed'))
  } finally { saving.value = false }
}

async function toggleRule(rule, enabled) {
  try {
    await updateAlertRule(rule.id, { enabled })
    ElMessage.success(enabled ? t('monitor.alert_rule.enabled_success') : t('monitor.alert_rule.disabled_success'))
    await loadData()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.alert_rule.operation_failed'))
  }
}

async function removeRule(rule) {
  const confirmed = await ElMessageBox.confirm(
    t('monitor.alert_rule.delete_confirm', { name: rule.name }),
    t('monitor.alert_rule.delete_title'),
    { confirmButtonText: t('monitor.alert_rule.delete'), cancelButtonText: t('common.cancel'), type: 'warning', confirmButtonClass: 'el-button--danger' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  try {
    await deleteAlertRule(rule.id)
    ElMessage.success(t('monitor.alert_rule.deleted'))
    await loadData()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('monitor.alert_rule.operation_failed'))
  }
}

onMounted(loadData)
</script>

<style scoped>
.alert-rule-list { padding: 0 20px 20px; background: var(--addp-bg-secondary); }
.card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.page-title { color: var(--addp-text-primary); font-weight: 500; font-size: 16px; }
.page-description, .form-help, .target-identity, .muted { color: var(--addp-text-tertiary); font-size: 12px; }
.page-description, .form-help { margin-top: 6px; line-height: 1.5; }
.target-name { color: var(--addp-text-primary); }
.target-identity { margin-top: 4px; }
.route-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.route-options { display: flex; flex-direction: column; align-items: flex-start; }
.full-width { width: 100%; }
</style>
