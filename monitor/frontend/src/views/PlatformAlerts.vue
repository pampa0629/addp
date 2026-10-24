<template>
  <main class="platform-alerts">
    <h2>{{ t('monitor.platformAlerts.title') }}</h2>
    <div class="alert-toolbar">
      <el-button v-if="canRead" :type="state.tab === 'incidents' ? 'primary' : ''" @click="change({ tab: 'incidents' })">{{ t('monitor.platformAlerts.incidents') }}</el-button>
      <el-button v-if="canReadNotifications" :type="state.tab === 'notifications' ? 'primary' : ''" @click="change({ tab: 'notifications' })">{{ t('monitor.platformAlerts.notifications') }}</el-button>
      <el-button v-if="canReadRules" :type="state.tab === 'rules' ? 'primary' : ''" @click="change({ tab: 'rules' })">{{ t('monitor.resourceRules.title') }}</el-button>
      <el-button v-if="canReadRules" :type="state.tab === 'process-rules' ? 'primary' : ''" @click="change({ tab: 'process-rules' })">{{ t('monitor.processRules.title') }}</el-button>
      <el-button v-if="canReadHTTPRules" :type="state.tab === 'http-rules' ? 'primary' : ''" @click="change({ tab: 'http-rules' })">{{ t('monitor.httpRules.title') }}</el-button>
      <el-button :loading="loading" @click="refresh">{{ t('monitor.platformAlerts.refresh') }}</el-button>
    </div>
    <el-alert v-if="!canRead && !canReadNotifications && !canReadRules && !canReadHTTPRules" :title="t('monitor.platformAlerts.denied')" type="warning" :closable="false" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <section v-if="state.tab === 'incidents' && canRead">
      <div class="alert-toolbar">
        <el-select :model-value="state.category" @update:model-value="value => change({ category: value, page: 1 })" :aria-label="t('monitor.platformAlerts.category')">
          <el-option :label="t('monitor.platformAlerts.allCategories')" value="" /><el-option v-for="category in ['log_pipeline','resource','http_quality','process_resource']" :key="category" :label="categoryLabel(category)" :value="category" />
        </el-select>
        <el-select :model-value="state.status" @update:model-value="value => change({ status: value, page: 1 })" :aria-label="t('monitor.platformAlerts.status')">
          <el-option :label="t('monitor.platformAlerts.active')" value="active" /><el-option :label="t('monitor.platformAlerts.allStatuses')" value="" /><el-option v-for="status in ['open','acknowledged','resolved']" :key="status" :label="t(`monitor.platformAlerts.statuses.${status}`)" :value="status" />
        </el-select>
        <el-select :model-value="state.severity" @update:model-value="value => change({ severity: value, page: 1 })" :aria-label="t('monitor.platformAlerts.severity')">
          <el-option :label="t('monitor.platformAlerts.allSeverities')" value="" /><el-option v-for="severity in ['critical','warning']" :key="severity" :label="t(`monitor.platformAlerts.severities.${severity}`)" :value="severity" />
        </el-select>
        <el-input :model-value="state.subject_id" @change="value => change({ subject_id: value, page: 1 })" :placeholder="t('monitor.platformAlerts.subject')" maxlength="100" clearable />
      </div>
      <el-table :data="incidents" data-testid="pipeline-incidents">
        <el-table-column :label="t('monitor.platformAlerts.category')" width="130"><template #default="{ row }">{{ categoryLabel(row.category) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.signal')" min-width="180"><template #default="{ row }">{{ row.category === 'process_resource' ? processSignal(row) : row.category === 'http_quality' ? httpSignal(row) : signalLabel(row.signal) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.subject')" min-width="220" show-overflow-tooltip><template #default="{ row }"><span :title="row.subject_id">{{ processSubject(row) || hosts[resourceAlertHostID(row)]?.display_name || row.subject_id }}</span></template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.details')" width="130"><template #default="{ row }"><el-button v-if="resourceAlertHostID(row) || ['http_quality','process_resource'].includes(row.category)" size="small" @click="selectedIncident = row">{{ t('monitor.platformAlerts.openEvidence') }}</el-button></template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.severity')" width="100"><template #default="{ row }"><el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'">{{ t(`monitor.platformAlerts.severities.${row.severity}`) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.status')" width="130"><template #default="{ row }">{{ t(`monitor.platformAlerts.statuses.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.openedAt')" min-width="160"><template #default="{ row }">{{ date(row.opened_at) }}</template></el-table-column>
        <el-table-column v-if="canUpdate" :label="t('monitor.platformAlerts.actions')" width="190"><template #default="{ row }">
          <el-button size="small" :disabled="row.status === 'acknowledged' || row.status === 'resolved' || busy" @click="manage(row, 'acknowledge')">{{ t('monitor.platformAlerts.acknowledge') }}</el-button>
          <el-button size="small" :disabled="busy || row.status === 'resolved'" @click="manage(row, 'suppress')">{{ t('monitor.platformAlerts.suppress') }}</el-button>
          <small v-if="row.suppressed_until">{{ t('monitor.platformAlerts.suppressedUntil', { time: date(row.suppressed_until) }) }}</small>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="!loading && total > 0" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="state.page" @current-change="value => change({ page: value })" />
    </section>
    <section v-if="state.tab === 'notifications' && canReadNotifications" data-testid="platform-notifications">
      <el-alert v-if="notificationError" :title="notificationError" type="error" :closable="false" />
      <el-button v-if="canUpdateNotifications" @click="editDestination()">{{ t('monitor.platformAlerts.newDestination') }}</el-button>

      <el-table :data="destinations">
        <el-table-column :label="t('monitor.platformAlerts.category')"><template #default="{ row }">{{ (row.categories || []).map(categoryLabel).join(' / ') }}</template></el-table-column>
        <el-table-column prop="name" :label="t('monitor.platformAlerts.name')" />
        <el-table-column :label="t('monitor.platformAlerts.channel')"><template #default="{ row }">{{ channelLabel(row.channel) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.target')" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.channel === 'email' ? row.recipients.join(', ') : row.url }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.enabled')" width="90"><template #default="{ row }">{{ fact(row.enabled) }}</template></el-table-column>
        <el-table-column v-if="canUpdateNotifications" :label="t('monitor.platformAlerts.actions')" width="260"><template #default="{ row }">
          <el-button size="small" @click="editDestination(row)">{{ t('monitor.platformAlerts.edit') }}</el-button>
          <el-button size="small" :loading="busy" @click="testDestination(row)">{{ t('monitor.platformAlerts.test') }}</el-button>
          <el-button size="small" @click="deleteDestination(row)">{{ t('monitor.platformAlerts.delete') }}</el-button>
        </template></el-table-column>
      </el-table>
      <h3>{{ t('monitor.platformAlerts.deliveries') }}</h3>
      <el-table :data="deliveries.data" data-testid="pipeline-deliveries">
        <el-table-column type="expand" width="60">
          <template #default="{ row }">
            <el-descriptions :column="2" border class="delivery-diagnostics" data-testid="delivery-diagnostics">
              <el-descriptions-item :label="t('monitor.platformAlerts.eventID')">{{ row.event_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.targetID')">{{ row.destination_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.eventType')">{{ row.event_type }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.occurredAt')">{{ date(row.occurred_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.incidentID')">{{ row.incident_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.incidentStatus')">{{ t(`monitor.platformAlerts.statuses.${row.incident_status}`) }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.nextAttemptAt')">{{ date(row.next_attempt_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('monitor.platformAlerts.deliveredAt')">{{ date(row.delivered_at) }}</el-descriptions-item>
            </el-descriptions>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.category')"><template #default="{ row }">{{ categoryLabel(row.category) }}</template></el-table-column>
        <el-table-column prop="id" :label="t('monitor.platformAlerts.deliveryID')" min-width="160" show-overflow-tooltip />
        <el-table-column :label="t('monitor.platformAlerts.target')" min-width="160" show-overflow-tooltip><template #default="{ row }">{{ destinationLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.channel')"><template #default="{ row }">{{ channelLabel(row.channel) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.status')"><template #default="{ row }">{{ t(`monitor.platformAlerts.deliveryStatuses.${row.status}`) }}</template></el-table-column>
        <el-table-column prop="attempt_count" :label="t('monitor.platformAlerts.attempts')" />
        <el-table-column prop="cycle_attempt_count" :label="t('monitor.platformAlerts.cycleAttempts')" />
        <el-table-column prop="manual_retry_count" :label="t('monitor.platformAlerts.manualRetries')" />
        <el-table-column :label="t('monitor.platformAlerts.deliveryError')" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ deliveryError(row.last_error) }}</template></el-table-column>
        <el-table-column :label="t('monitor.platformAlerts.createdAt')" min-width="160"><template #default="{ row }">{{ date(row.created_at) }}</template></el-table-column>
        <el-table-column v-if="canUpdateNotifications" :label="t('monitor.platformAlerts.actions')" width="110" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status === 'dead'" size="small" :disabled="busy || !row.destination_version || (row.suppressed_until && new Date(row.suppressed_until) > new Date())" @click="retryDelivery(row)">{{ t('monitor.platformAlerts.retry') }}</el-button>
        </template></el-table-column>
      </el-table>
      <el-pagination v-if="!loading && deliveries.total > 0" layout="total, prev, pager, next" :total="deliveries.total" :page-size="20" :current-page="deliveryPage" @current-change="changeDeliveryPage" />
    </section>
    <el-dialog :model-value="Boolean(selectedIncident)" :title="t('monitor.platformAlerts.evidence.title')" width="min(720px, calc(100vw - 32px))" @close="selectedIncident = null">
      <section v-if="selectedIncident" data-testid="resource-alert-evidence">
        <h3 class="alert-subject">{{ processSubject(selectedIncident) || hosts[selectedIncident.subject_id]?.display_name || selectedIncident.subject_id }}</h3>
        <p>{{ selectedIncident.category === 'process_resource' ? processSignal(selectedIncident) : selectedIncident.category === 'http_quality' ? httpSignal(selectedIncident) : signalLabel(selectedIncident.signal) }}</p>
        <p v-if="resourceAlertHostID(selectedIncident) && canReadHosts && !hosts[selectedIncident.subject_id]">{{ t('monitor.platformAlerts.evidence.hostUnavailable') }}</p>
        <template v-if="selectedEvidence">
          <p>{{ t('monitor.platformAlerts.evidence.frozen') }}</p>
          <el-descriptions :column="1" border>
            <el-descriptions-item :label="t('monitor.resourceRules.trigger')">{{ t('monitor.platformAlerts.evidence.above', { percent: number(selectedEvidence.policy.trigger_percent), duration: duration(selectedEvidence.policy.trigger_seconds) }) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.resourceRules.recovery')">{{ t('monitor.platformAlerts.evidence.below', { percent: number(selectedEvidence.policy.recovery_percent), duration: duration(selectedEvidence.policy.recovery_seconds) }) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.latest')">{{ percent(selectedEvidence.latest) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.minimum')">{{ percent(selectedEvidence.minimum) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.maximum')">{{ percent(selectedEvidence.maximum) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.samples')">{{ selectedEvidence.samples }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.sampleWindow')">{{ evidenceDate(selectedEvidence.sampled_from) }} — {{ evidenceDate(selectedEvidence.sampled_to) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.evaluatedAt')">{{ evidenceDate(selectedEvidence.evaluated_at) }}</el-descriptions-item>
          </el-descriptions>
        </template>
        <template v-else-if="selectedThresholdEvidence">
          <p>{{ t('monitor.platformAlerts.evidence.frozen') }}</p>
          <el-descriptions :column="1" border data-testid="http-alert-evidence">
            <el-descriptions-item v-if="!selectedProcessEvidence" :label="t('monitor.httpRules.subject')">{{ selectedThresholdEvidence.module_name }} · {{ t(`monitor.http.layers.${selectedThresholdEvidence.role}`) }} · {{ selectedThresholdEvidence.route === null ? t('monitor.http.allRoutes') : `${selectedThresholdEvidence.method} ${selectedThresholdEvidence.route || t('monitor.http.unmatched')}` }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.resourceRules.trigger')">{{ t('monitor.httpRules.above', { value: thresholdValue(selectedThresholdEvidence.policy.trigger_value), seconds: selectedThresholdEvidence.policy.trigger_seconds }) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.resourceRules.recovery')">{{ t('monitor.httpRules.below', { value: thresholdValue(selectedThresholdEvidence.policy.recovery_value), seconds: selectedThresholdEvidence.policy.recovery_seconds }) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.latest')">{{ thresholdValue(selectedThresholdEvidence.latest) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.minimum')">{{ thresholdValue(selectedThresholdEvidence.minimum) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.maximum')">{{ thresholdValue(selectedThresholdEvidence.maximum) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.samples')">{{ selectedThresholdEvidence.samples }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.sampleWindow')">{{ evidenceDate(selectedThresholdEvidence.sampled_from) }} — {{ evidenceDate(selectedThresholdEvidence.sampled_to) }}</el-descriptions-item>
            <el-descriptions-item :label="t('monitor.platformAlerts.evidence.evaluatedAt')">{{ evidenceDate(selectedThresholdEvidence.evaluated_at) }}</el-descriptions-item>
          </el-descriptions>
        </template>
        <el-alert v-else :title="t('monitor.platformAlerts.evidence.unavailable')" type="warning" :closable="false" />
        <div class="alert-toolbar" v-if="canObserveHost(selectedIncident)">
          <el-button v-if="selectedEvidence" type="primary" @click="openHost(selectedIncident, true)">{{ t('monitor.platformAlerts.evidence.trend') }}</el-button>
          <el-button @click="openHost(selectedIncident)">{{ t('monitor.platformAlerts.evidence.current') }}</el-button>
        </div>
      </section>
    </el-dialog>
    <InstanceAlertRules kind="process" v-if="state.tab === 'process-rules' && canReadRules" :key="ruleIdentity" ref="processRulesPanel" :page="state.process_rule_page" @page="change({ process_rule_page: $event })" />
    <InstanceAlertRules kind="http" v-if="state.tab === 'http-rules' && canReadHTTPRules" :key="ruleIdentity" ref="httpRulesPanel" :page="state.http_rule_page" @page="change({ http_rule_page: $event })" />
    <ResourceAlertRules v-if="state.tab === 'rules' && canReadRules" :key="ruleIdentity" ref="rulesPanel" :page="state.rule_page" @page="change({ rule_page: $event })" />
    <el-dialog v-model="destinationOpen" :title="t('monitor.platformAlerts.destination')" width="min(640px, 95vw)" :close-on-click-modal="false">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top">
        <el-form-item :label="t('monitor.platformAlerts.name')"><el-input v-model="destinationDraft.name" maxlength="100" /></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.channel')"><el-select v-model="destinationDraft.channel" :disabled="!!editingID"><el-option label="Webhook" value="webhook" /><el-option :label="t('monitor.platformAlerts.wecom')" value="wecom" /><el-option :label="t('monitor.platformAlerts.email')" value="email" /></el-select></el-form-item>
        <el-form-item v-if="destinationDraft.channel === 'webhook'" :label="t('monitor.platformAlerts.url')"><el-input v-model="destinationDraft.url" maxlength="2048" /></el-form-item>
        <el-form-item v-else-if="destinationDraft.channel === 'email'" :label="t('monitor.platformAlerts.recipients')"><el-input v-model="recipientsText" /></el-form-item>
        <el-form-item v-if="destinationDraft.channel !== 'email'" :label="t(destinationDraft.channel === 'wecom' ? 'monitor.platformAlerts.wecomCredential' : 'monitor.platformAlerts.secret')"><el-input v-model="secret" type="password" show-password autocomplete="new-password" /></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.sourceCategories')"><el-checkbox-group v-model="destinationDraft.categories"><el-checkbox v-for="category in ['log_pipeline','resource','http_quality','process_resource']" :key="category" :value="category">{{ t(`monitor.platformAlerts.categories.${category}`) }}</el-checkbox></el-checkbox-group></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.events')"><el-checkbox-group v-model="destinationDraft.event_types"><el-checkbox v-for="event in ['opened','resolved']" :key="event" :value="event">{{ t(`monitor.platformAlerts.eventsLabel.${event}`) }}</el-checkbox></el-checkbox-group></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.enabled')"><el-switch v-model="destinationDraft.enabled" /></el-form-item>
      </el-form>
      <template #footer><el-button :loading="busy" type="primary" @click="saveDestination">{{ t('monitor.platformAlerts.save') }}</el-button></template>
    </el-dialog>
  </main>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { platformAlertsAPI as api } from '../api/platformAlerts'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
import { platformAlertState, platformAlertQuery, resourceAlertHostID, resourceAlertEvidence, resourceAlertTrendRoute } from '../utils/platformAlerts'
import { loadHostReferences } from '../utils/hostReferences'
import { nodeResourcesAPI } from '../api/nodeResources'
import { formatDurationSeconds } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import ResourceAlertRules from '../components/ResourceAlertRules.vue'
import InstanceAlertRules from '../components/InstanceAlertRules.vue'
import { processAlertEvidence, processRuleMetrics, processRuleScale } from '../utils/processRules'
import { httpAlertEvidence, httpRuleMetrics } from '../utils/httpRules'
const route = useRoute(), router = useRouter(), auth = useAuthStore(), { t, locale } = useI18n()
const state = computed(() => platformAlertState(route.query))
const platformUser = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation)
const canRead = computed(() => platformUser.value && auth.hasPermission('monitor.platform_alert.read'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('monitor.platform_alert.update'))
const canReadNotifications = computed(() => platformUser.value && auth.hasPermission('monitor.platform_alert_notification.read'))
const canUpdateNotifications = computed(() => canReadNotifications.value && auth.hasPermission('monitor.platform_alert_notification.update'))
const canReadRules = computed(() => platformUser.value && auth.hasPermission('monitor.platform_resource_rule.read'))
const canReadHTTPRules = computed(() => platformUser.value && auth.hasPermission('monitor.platform_http_rule.read'))
const canReadHosts = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const hosts = ref({}), selectedIncident = ref(null)
const selectedEvidence = computed(() => resourceAlertEvidence(selectedIncident.value))
const evidenceDate = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const number = value => formatFieldPresentationValue(value, { precision: 2 }, locale.value)
const percent = value => formatFieldPresentationValue(value, { precision: 2, unit: '%' }, locale.value)
const duration = value => formatDurationSeconds(value, locale.value)
function canObserveHost(row) { return canReadHosts.value && auth.hasPermission('monitor.resource_observation.read') && Boolean(hosts.value[resourceAlertHostID(row)]) }
function openHost(row, historical = false) {
  if (!canObserveHost(row)) return
  const target = historical ? resourceAlertTrendRoute(row) : { path: `/node-resources/${row.subject_id}`, query: {} }
  if (target) navigateMonitorRoute(router, target)
}
const rulesPanel = ref(null), httpRulesPanel = ref(null), processRulesPanel = ref(null)
function refresh() { if (state.value.tab === 'rules') rulesPanel.value?.load(); else if (state.value.tab === 'process-rules') processRulesPanel.value?.load(); else if (state.value.tab === 'http-rules') httpRulesPanel.value?.load(); else load() }
const selectedProcessEvidence = computed(() => processAlertEvidence(selectedIncident.value))
const selectedThresholdEvidence = computed(() => selectedProcessEvidence.value || httpAlertEvidence(selectedIncident.value))
const thresholdValue = value => selectedProcessEvidence.value ? processValue(value,selectedProcessEvidence.value.metric_key) : number(value)
function processValue(value,key) { return `${number(value / processRuleScale(key))} ${key === 'process.memory.resident_bytes' ? 'GiB' : t('monitor.processRules.cores')}` }
function processSubject(row) { const e=processAlertEvidence(row); return e ? `${e.module_name} · ${t(`monitor.process.roles.${e.role}`)} · ${e.runtime_instance_id}` : '' }
function processSignal(row) { const e=processAlertEvidence(row); return e ? t(`monitor.processRules.${processRuleMetrics.find(m=>m.key===e.metric_key)?.name}`) : t('monitor.processRules.title') }
function httpSignal(row) { const e = httpAlertEvidence(row); return e ? t(`monitor.http.metrics.${httpRuleMetrics.find(metric => metric.key === e.metric_key)?.name}`) : t('monitor.httpRules.title') }
const ruleIdentity = computed(() => JSON.stringify([auth.authContext, auth.permissions]))
const incidents = ref([]), total = ref(0), loading = ref(false), busy = ref(false), error = ref('')
const destinations = ref([]), deliveries = ref({ data: [], total: 0 }), notificationError = ref('')
const deliveryPage = computed(() => state.value.delivery_page)
const destinationOpen = ref(false), destinationDraft = ref({ categories: [], recipients: [], event_types: [] }), editingID = ref(0), recipientsText = ref(''), secret = ref(''), editError = ref('')
const date = value => value ? new Date(value).toLocaleString() : '—'
const categoryLabel = value => ['log_pipeline', 'resource', 'http_quality', 'process_resource'].includes(value) ? t(`monitor.platformAlerts.categories.${value}`) : '—'
const channelLabel = value => value === 'wecom' ? t('monitor.platformAlerts.wecom') : value === 'email' ? t('monitor.platformAlerts.email') : 'Webhook'
const destinationLabel = row => row.destination_name || t('monitor.platformAlerts.unavailableTarget', { id: row.destination_id })
const fact = value => t(`monitor.platformAlerts.facts.${value ? 'yes' : 'no'}`)
const signalLabel = value => value?.startsWith('node.') ? ({ 'node.cpu.busy_percent': t('monitor.platformAlerts.cpuPressure'), 'node.memory.used_percent': t('monitor.platformAlerts.memoryPressure') })[value] || value : t(`monitor.platformAlerts.signals.${value?.split(':')[0]}`)
const errorText = value => value.response?.data?.error || t('monitor.platformAlerts.failed')
const deliveryError = code => {
  if (!code) return '—'
  const known = ['notification_send_failed', 'notification_attempt_limit', 'wecom_rate_limited', 'wecom_rejected', 'wecom_invalid_response', 'wecom_network_failed', 'wecom_http_failed']
  return t(`monitor.platformAlerts.deliveryErrors.${known.includes(code) ? code : 'unknown'}`)
}
let epoch = 0, request = 0, controller, disposed = false
const abandoned = Symbol('abandoned platform operation')
function check(lease) { if (lease !== epoch || disposed) throw abandoned }
function clear() {
  epoch++; request++; controller?.abort(); incidents.value = []; total.value = 0; hosts.value = {}; selectedIncident.value = null
  destinations.value = []; deliveries.value = { data: [], total: 0 }; error.value = ''; notificationError.value = ''; loading.value = false
  busy.value = false; destinationOpen.value = false; secret.value = ''; destinationDraft.value = { categories: [], recipients: [], event_types: [] }; editError.value = ''
}
function change(patch) {
  const next = { ...state.value, ...patch }
  navigateMonitorRoute(router, { path: '/platform-alerts', query: platformAlertQuery(next) }, { history: 'replace' })
}
function changeDeliveryPage(page) { change({ delivery_page: page }) }
async function load() {
  if (['rules','http-rules','process-rules'].includes(state.value.tab)) return
  controller?.abort(); controller = new AbortController(); const own = ++request, lease = epoch
  if (state.value.tab === 'notifications' ? !canReadNotifications.value : !canRead.value) return
  loading.value = true; error.value = ''; notificationError.value = ''; hosts.value = {}; selectedIncident.value = null; incidents.value = []
  try {
    if (state.value.tab === 'notifications') {
      const [targets, attempts] = await Promise.all([api.destinations(controller.signal), api.deliveries(deliveryPage.value, controller.signal)])
      check(lease); if (own !== request) return
      destinations.value = targets; deliveries.value = attempts
    } else {
      const { page, category, status, subject_id, severity } = state.value
      const result = await api.incidents({ page, page_size: 20, ...(category ? { category } : {}), ...(status ? { status } : {}), ...(subject_id ? { subject_id } : {}), ...(severity ? { severity } : {}) }, controller.signal)
      check(lease); if (own !== request) return
      incidents.value = result.data; total.value = result.total
      if (canReadHosts.value) {
        const signal = controller.signal
        const names = await loadHostReferences(result.data.map(resourceAlertHostID), id => nodeResourcesAPI.node(id, { signal }), () => !disposed && lease === epoch && own === request)
        check(lease); if (own !== request) return
        hosts.value = names
      }
    }
  } catch (value) {
    if (lease !== epoch || own !== request || value === abandoned || value.code === 'ERR_CANCELED') return
    incidents.value = []; total.value = 0; destinations.value = []; deliveries.value = { data: [], total: 0 }
    if (state.value.tab === 'notifications') notificationError.value = errorText(value); else error.value = errorText(value)
  } finally { if (lease === epoch && own === request) loading.value = false }
}
async function manage(row, action) {
  if (busy.value || !canUpdate.value) return
  const lease = epoch; busy.value = true
  try { if (action === 'acknowledge') await api.acknowledge(row); else await api.suppress(row, new Date(Date.now() + 3600000).toISOString()); check(lease); await load() }
  catch (value) { if (lease === epoch && value !== abandoned) ElMessage.error(errorText(value)) }
  finally { if (lease === epoch) busy.value = false }
}
function editDestination(row) {
  if (!canUpdateNotifications.value) return
  editingID.value = row?.id || 0
  destinationDraft.value = row ? { version: row.version, name: row.name, channel: row.channel, url: row.channel === 'webhook' ? row.url || '' : '', categories: [...row.categories], recipients: [...row.recipients], event_types: [...row.event_types], enabled: row.enabled } : { name: '', channel: 'webhook', url: '', categories: [], recipients: [], event_types: ['opened', 'resolved'], enabled: false }
  recipientsText.value = destinationDraft.value.recipients.join(', '); secret.value = ''; editError.value = ''; destinationOpen.value = true
}
async function saveDestination() {
  if (busy.value || !canUpdateNotifications.value) return
  const lease = epoch; busy.value = true; editError.value = ''
  try {
    const data = { ...destinationDraft.value, recipients: destinationDraft.value.channel === 'email' ? recipientsText.value.split(',').map(value => value.trim()).filter(Boolean) : [], url: destinationDraft.value.channel === 'webhook' ? destinationDraft.value.url : '' }
    if (!data.categories.length) { editError.value = t('monitor.platformAlerts.categoryRequired'); return }
    const enabled = data.enabled
    if (!editingID.value && data.channel !== 'email') data.enabled = false
    let row
    if (editingID.value && secret.value && data.channel !== 'email') {
      row = await api.credential({ id: editingID.value, version: data.version }, secret.value); check(lease)
      secret.value = ''; destinationDraft.value.version = row.version; data.version = row.version
    }
    row = editingID.value ? await api.updateDestination(editingID.value, data) : await api.createDestination(data); check(lease)
    editingID.value = row.id; destinationDraft.value.version = row.version
    if (secret.value && row.channel !== 'email') { row = await api.credential(row, secret.value); check(lease); secret.value = ''; destinationDraft.value.version = row.version }
    if (enabled && !row.enabled) { row = await api.updateDestination(row.id, { ...data, version: row.version, enabled }); check(lease); destinationDraft.value.version = row.version }
    destinationOpen.value = false; await load()
  } catch (value) { if (lease === epoch && value !== abandoned) editError.value = errorText(value) }
  finally { if (lease === epoch) busy.value = false }
}
async function testDestination(row) {
  if (busy.value || !canUpdateNotifications.value) return
  const lease = epoch; busy.value = true
  try { await api.testDestination(row); check(lease); ElMessage.success(t('monitor.platformAlerts.testSent')) }
  catch (value) { if (lease === epoch && value !== abandoned) ElMessage.error(errorText(value)) }
  finally { if (lease === epoch) busy.value = false }
}
async function retryDelivery(row) {
  if (busy.value || !canUpdateNotifications.value) return
  const lease = epoch; busy.value = true
  try {
    const history = row.incident_status === 'resolved' ? `${t('monitor.platformAlerts.retryHistorical')} ` : ''
    await ElMessageBox.confirm(history + t('monitor.platformAlerts.retryConfirm', { target: destinationLabel(row), event: row.event_type, time: date(row.occurred_at), status: t(`monitor.platformAlerts.statuses.${row.incident_status}`) }), t('monitor.platformAlerts.retry')); check(lease)
    notificationError.value = ''; await api.retryDelivery(row); check(lease)
    ElMessage.success(t('monitor.platformAlerts.requeued')); await load()
  } catch (value) { if (lease === epoch && ![abandoned, 'cancel', 'close'].includes(value)) notificationError.value = errorText(value) }
  finally { if (lease === epoch) busy.value = false }
}
async function deleteDestination(row) {
  if (busy.value || !canUpdateNotifications.value) return
  const lease = epoch; busy.value = true
  try {
    await ElMessageBox.confirm(t('monitor.platformAlerts.deleteConfirm', { name: row.name }), t('monitor.platformAlerts.delete')); check(lease)
    await api.deleteDestination(row); check(lease); await load()
  } catch (value) { if (lease === epoch && ![abandoned, 'cancel', 'close'].includes(value)) ElMessage.error(errorText(value)) }
  finally { if (lease === epoch) busy.value = false }
}
watch(() => [route.fullPath, JSON.stringify(auth.authContext), JSON.stringify(auth.permissions)], () => {
  clear()
  const next = platformAlertQuery({ ...state.value, ...((state.value.tab === 'incidents' && !canRead.value) || (state.value.tab === 'notifications' && !canReadNotifications.value) || (state.value.tab === 'rules' && !canReadRules.value) || (state.value.tab === 'http-rules' && !canReadHTTPRules.value) ? { tab: canRead.value ? 'incidents' : canReadNotifications.value ? 'notifications' : canReadRules.value ? 'rules' : canReadHTTPRules.value ? 'http-rules' : 'incidents' } : {}) })
  if (JSON.stringify(route.query) !== JSON.stringify(next)) { navigateMonitorRoute(router, { path: '/platform-alerts', query: next }, { history: 'replace' }); return }
  load()
}, { immediate: true })
onUnmounted(() => { disposed = true; clear() })
watch(destinationOpen, value => { if (!value) secret.value = '' })
</script>
<style scoped>
.alert-subject { overflow-wrap: anywhere; }
.platform-alerts { padding: 20px; color: var(--addp-text-primary); }
.alert-toolbar { display: flex; gap: 12px; flex-wrap: wrap; margin: 16px 0; }
.alert-toolbar .el-select { width: 180px; }
.alert-toolbar .el-input { width: 280px; }
.delivery-diagnostics { margin: 12px; }
small { display: block; color: var(--addp-text-secondary); margin-top: 6px; }
</style>
