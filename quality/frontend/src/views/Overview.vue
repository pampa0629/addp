<template>
  <div class="overview">
    <div class="page-header">
      <div><h2>{{ t('quality.overview.title') }}</h2><p>{{ t('quality.overview.help') }}</p></div>
      <MonitorExecutionsButton module="quality" task-type="quality_plan" />
    </div>
    <el-form inline>
      <el-form-item :label="t('quality.domain.owner')">
        <DomainOwnershipSelect :model-value="state.ownerDomainID" filter :can-read="can('standard.domain.read')" @loaded="domains = $event" @update:model-value="value => change({ ownerDomainID: value, page: 1 })" />
      </el-form-item>
      <el-form-item :label="t('quality.overview.window')">
        <el-select :model-value="days" class="window-select" @update:model-value="value => change({ days: value, page: 1 })">
          <el-option v-for="day in [7, 30, 90]" :key="day" :value="day" :label="t('quality.overview.days', { days: day })" />
        </el-select>
      </el-form-item>
      <el-button :loading="loading" @click="load">{{ t('quality.overview.refresh') }}</el-button>
    </el-form>
    <el-alert v-if="error" type="error" :title="error" :closable="false" />
    <template v-else-if="summary">
      <div class="metrics">
        <el-card v-for="key in ['plan_count', 'never_run_plans', 'open_issues']" :key="key" shadow="never">
          <el-statistic :title="t(`quality.overview.${key}`)" :value="summary[key]" />
        </el-card>
      </div>
      <el-alert v-if="summary.unscoped_issues || summary.unscoped_executions" type="warning" :closable="false" :title="t('quality.overview.unrecorded', { issues: summary.unscoped_issues, executions: summary.unscoped_executions })" />
      <h3>{{ t('quality.overview.current') }}</h3>
      <el-table :data="summary.data" border v-loading="loading">
        <el-table-column :label="t('quality.plan.name')" min-width="180">
          <template #default="{ row }">{{ row.plan_name }} · V{{ row.plan_version }}</template>
        </el-table-column>
        <el-table-column :label="t('quality.domain.owner')" min-width="140">
          <template #default="{ row }">{{ domainOwnershipLabel(row.owner_domain_id, t, domains) }}</template>
        </el-table-column>
        <el-table-column :label="t('quality.targets.actual')" min-width="240">
          <template #default="{ row }">
            <div v-for="binding in row.table_bindings || []" :key="binding.alias">{{ binding.alias }}: {{ targetLabel(binding.locator) }}</div>
            <span v-if="!row.target_key">{{ t('quality.overview.noScope') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('quality.overview.latestAttempt')" min-width="150">
          <template #default="{ row }">
            <el-button v-if="row.execution_id" link type="primary" @click="openExecution(row.execution_id)">{{ t(`quality.execution.${row.status}`) }}</el-button>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('quality.overview.observation')" min-width="220">
          <template #default="{ row }">
            <template v-if="row.observed_execution_id">
              <el-button link type="primary" @click="openExecution(row.observed_execution_id)">{{ rate(row.pass_rate) }} ({{ row.passed_rules }}/{{ row.total_rules }})</el-button>
              <div>{{ date(row.observed_at) }} · V{{ row.observed_version }}</div>
              <el-tag v-if="row.observed_version !== row.plan_version" type="warning">{{ t('quality.overview.olderDefinition') }}</el-tag>
            </template>
            <span v-else>{{ t('quality.overview.noObservation') }}</span>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination :current-page="state.page" :page-size="state.pageSize" :total="summary.total" layout="total, prev, pager, next" @current-change="page => change({ page })" />
      <h3>{{ t('quality.overview.trend') }}</h3>
      <p>{{ t('quality.overview.trendHelp') }}</p>
      <el-table :data="summary.trend" border>
        <el-table-column prop="day" :label="t('quality.overview.day')" />
        <el-table-column prop="executions" :label="t('quality.overview.attempts')" />
        <el-table-column prop="runtime_errors" :label="t('quality.overview.runtimeErrors')" />
        <el-table-column prop="passed_rules" :label="t('quality.overview.passedRules')" />
        <el-table-column prop="total_rules" :label="t('quality.overview.totalRules')" />
        <el-table-column :label="t('quality.overview.passRate')"><template #default="{ row }">{{ rate(row.pass_rate) }}</template></el-table-column>
      </el-table>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { MonitorExecutionsButton, formatLocatorDisplayPath, parseLocatorSafe } from '@common-ui'
import { overviewAPI } from '../api/quality'
import { useAuthStore } from '../store/auth'
import DomainOwnershipSelect from '../components/DomainOwnershipSelect.vue'
import { domainOwnershipLabel } from '../utils/domainOwnership'
import { resolvePlanRouteState, buildPlanRouteQuery } from '../utils/planRouteState'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { executionDetailRoute } from '../utils/executionNavigation'

const { t } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore()
const can = permission => auth.hasPermission(permission)
const state = computed(() => resolvePlanRouteState(route.query))
const days = computed(() => [7,30,90].includes(Number(route.query.days)) ? Number(route.query.days) : 30)
const summary = ref(null), domains = ref([]), error = ref(''), loading = ref(false)
let sequence = 0
const rate = value => value == null ? '—' : `${Number(value).toFixed(1)}%`
const date = value => value ? new Date(value).toLocaleString() : '—'
const targetLabel = locator => `${t('quality.targets.engine', { id: parseLocatorSafe(locator).engineId })} · ${formatLocatorDisplayPath(locator)}`
const openExecution = id => navigateQualityRoute(router, executionDetailRoute(id), { history: 'push' })
const change = values => {
  const next = { ...state.value, days: days.value, ...values, mode: 'list' }
  return navigateQualityRoute(router, { path: '/overview', query: { ...buildPlanRouteQuery(next), ...(next.days !== 30 ? { days: String(next.days) } : {}) } }, { history: 'push' })
}
async function load() {
  const current = ++sequence
  loading.value = true; error.value = ''
  try {
    const result = await overviewAPI.get({ days: days.value, page: state.value.page, page_size: state.value.pageSize, ...(state.value.ownerDomainID != null ? { owner_domain_id: state.value.ownerDomainID } : {}) })
    if (current === sequence) summary.value = result
  } catch (e) {
    if (current === sequence) { summary.value = null; error.value = e.response?.data?.error || t('quality.plan.loadFailed') }
  } finally { if (current === sequence) loading.value = false }
}
watch(() => route.fullPath, load, { immediate: true })
onBeforeUnmount(() => { sequence++ })
</script>

<style scoped>
.overview { padding: 24px; background: var(--addp-bg-primary); color: var(--addp-text-primary); }
.page-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin: 16px 0; }
.window-select { width: 160px; }
.el-pagination { margin-top: 16px; justify-content: flex-end; }
p { color: var(--addp-text-secondary); }
</style>
