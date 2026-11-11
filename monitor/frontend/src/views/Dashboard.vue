<template>
  <div class="dashboard">
    <!-- 统计卡片行 -->
    <el-row :gutter="20" style="margin-bottom: 20px;">
      <el-col :span="6">
        <statistics-card
          :title="t('monitor.dashboard.total_executions')"
          :value="stats.total || 0"
          icon="Document"
          type="primary"
        />
      </el-col>
      <el-col :span="6">
        <statistics-card
          :title="t('monitor.dashboard.success_rate')"
          :value="`${(stats.success_rate || 0).toFixed(1)}%`"
          :subtitle="`${t('monitor.dashboard.success_count')}: ${stats.success_count || 0}`"
          icon="SuccessFilled"
          type="success"
        />
      </el-col>
      <el-col :span="6">
        <statistics-card
          :title="t('monitor.dashboard.running')"
          :value="stats.running_count || 0"
          icon="Loading"
          type="warning"
        />
      </el-col>
      <el-col :span="6">
        <statistics-card
          :title="t('monitor.dashboard.failed_count')"
          :value="stats.failed_count || 0"
          icon="CircleClose"
          type="danger"
        />
      </el-col>
    </el-row>

    <!-- 执行趋势图表 -->
    <el-card shadow="hover" class="trend-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.dashboard.trend_title') }}</span>
          <el-select v-model="trendDays" @change="loadTrendData" style="width: 100px;">
            <el-option :label="t('monitor.dashboard.trend_days.7')" :value="7" />
            <el-option :label="t('monitor.dashboard.trend_days.14')" :value="14" />
            <el-option :label="t('monitor.dashboard.trend_days.30')" :value="30" />
          </el-select>
        </div>
      </template>
      <div ref="trendChart" class="trend-chart" v-loading="loadingTrend"></div>
    </el-card>

    <el-card shadow="hover" class="runtime-overview">
      <template #header>
        <div class="card-header overview-header">
          <span>{{ t('monitor.dashboard.overview.title') }}</span>
          <div class="runtime-metrics-actions">
            <el-select v-model="runtimeMetricsDuration" style="width: 150px;" @change="loadRuntimeMetrics"
              :aria-label="t('monitor.dashboard.overview.window')">
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.24h')" value="24h" />
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.7d')" value="7d" />
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.30d')" value="30d" />
            </el-select>
            <el-button text @click="refreshOverview" :loading="loadingModules || loadingRuntimeHealth || loadingRuntimeMetrics">
              <el-icon><Refresh /></el-icon>{{ t('monitor.dashboard.refresh') }}
            </el-button>
          </div>
        </div>
      </template>
      <div class="overview-errors">
        <el-alert v-if="modulesError" :title="t('monitor.dashboard.modules_failed')" type="error" :closable="false" />
        <el-alert v-if="runtimeHealthError" :title="t('monitor.dashboard.runtime_health.load_failed')" type="error" :closable="false" />
        <el-alert v-if="runtimeMetricsError" :title="t('monitor.dashboard.runtime_metrics.load_failed')" type="error" :closable="false" />
      </div>
      <div class="module-grid" :aria-busy="loadingModules || loadingRuntimeHealth || loadingRuntimeMetrics">
        <article v-for="card in moduleCards" :key="card.module" class="module-runtime-card"
          :data-module="card.module">
          <div class="module-card-heading">
            <h3>{{ card.module }}</h3>
            <el-button text aria-haspopup="dialog" @click="detailModule = card.module">
              {{ t('monitor.dashboard.overview.view_details') }}
              <el-icon><ArrowRight /></el-icon>
            </el-button>
          </div>
          <dl class="module-summary">
            <template v-if="canReadHealth">
              <div>
                <dt>{{ t('monitor.dashboard.overview.backend') }}</dt>
                <dd>
                  <el-tag v-if="card.provider" size="small" :type="providerTagType(card.provider.status)">
                    {{ providerStatusText(card.provider.status) }}
                  </el-tag>
                  <span v-else>{{ t(modulesError ? 'monitor.dashboard.overview.unavailable' : 'monitor.dashboard.overview.not_registered') }}</span>
                  <span v-if="card.provider?.backends?.some(item => item.module_health)" class="backend-count">
                    {{ card.provider.backends.filter(item => item.module_health?.status === 'up').length }}/{{ card.provider.backends.length }}
                  </span>
                  <span v-else-if="card.provider" class="backend-count">{{ t('monitor.dashboard.overview.unchecked') }}</span>
                </dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.overview.instances') }}</dt>
                <dd>{{ card.runtimes.length ? card.online : t(runtimeHealthError ? 'monitor.dashboard.overview.unavailable' : 'monitor.dashboard.overview.unreported') }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.overview.slots') }}</dt>
                <dd>{{ card.runtimes.length ? card.active + ' / ' + card.capacity : '—' }}</dd>
              </div>
            </template>
            <div>
              <dt>{{ t('monitor.dashboard.runtime_metrics.backlog') }}</dt>
              <dd>{{ card.metrics.length ? card.pending + ' / ' + card.running : '—' }}</dd>
            </div>
            <div>
              <dt>{{ t('monitor.dashboard.runtime_metrics.throughput') }}</dt>
              <dd>{{ card.metrics.length ? formatDecimal(card.throughput) : '—' }}</dd>
            </div>
            <div>
              <dt>{{ t('monitor.dashboard.runtime_metrics.failure_rate') }}</dt>
              <dd>{{ card.completed ? formatPercentage(card.failureRate) : '—' }}</dd>
            </div>
          </dl>

        </article>
      </div>
      <el-empty v-if="!moduleCards.length && !loadingModules && !loadingRuntimeHealth && !loadingRuntimeMetrics"
        :description="t('monitor.dashboard.no_modules')" />
    </el-card>

    <el-drawer :model-value="Boolean(detailCard)" :title="detailCard?.module"
      size="min(640px, 100vw)" destroy-on-close @close="detailModule = null">
      <div v-if="detailCard" class="module-details">
        <template v-if="canReadHealth">
          <h4>{{ t('monitor.dashboard.overview.components') }}</h4>
          <p v-if="detailCard.latency" class="card-description">{{ t('monitor.module_status.latency', { ms: detailCard.latency }) }}</p>
          <p v-if="detailCard.provider?.capabilities?.status && detailCard.provider.capabilities.status !== 'up'" class="card-description">
            {{ detailCard.provider.capabilities.message || t('monitor.module_status.capabilities_invalid') }}
          </p>
          <p v-if="!detailCard.provider?.backends?.length && detailCard.provider?.message" class="card-description">{{ detailCard.provider.message }}</p>
          <p v-if="!detailCard.runtimes.length" class="card-description">{{ t(runtimeHealthError ? 'monitor.dashboard.runtime_health.load_failed' : 'monitor.dashboard.runtime_health.empty') }}</p>
          <div class="runtime-detail-grid">
            <section v-for="row in detailCard.runtimes" :key="row.role + '/' + row.runtime_name" class="runtime-detail">
              <div class="runtime-detail-heading">
                <strong>{{ row.runtime_name }}</strong>
                <el-tag :type="runtimeStatusTagType(row.status)" size="small">{{ runtimeStatusText(row.status) }}</el-tag>
              </div>
              <div class="card-description">{{ runtimeRoleText(row.role) }}</div>
              <dl class="detail-values">
                <div>
                  <dt>{{ t('monitor.dashboard.overview.instances') }}</dt>
                  <dd>{{ row.healthy_instances }}</dd>
                </div>
                <div>
                  <dt>{{ t('monitor.dashboard.overview.known_instances') }}</dt>
                  <dd>{{ row.known_instances }}</dd>
                </div>
                <div>
                  <dt>{{ t('monitor.dashboard.overview.slots') }}</dt>
                  <dd>{{ row.active_count }} / {{ row.capacity }}</dd>
                </div>
                <div>
                  <dt>{{ t('monitor.dashboard.runtime_health.last_heartbeat') }}</dt>
                  <dd>{{ formatDate(row.last_heartbeat_at) }}</dd>
                </div>
                <div>
                  <dt>{{ t('monitor.dashboard.runtime_health.expires_at') }}</dt>
                  <dd>{{ formatDate(row.expires_at) }}</dd>
                </div>
              </dl>
            </section>
          </div>
        </template>
        <h4>{{ t('monitor.dashboard.overview.tasks') }}</h4>
        <p v-if="!detailCard.metrics.length" class="card-description">{{ t(runtimeMetricsError ? 'monitor.dashboard.runtime_metrics.load_failed' : 'monitor.dashboard.runtime_metrics.empty') }}</p>
        <div class="runtime-detail-grid">
          <section v-for="row in detailCard.metrics" :key="row.task_type + '/' + row.execution_boundary" class="runtime-detail">
            <div class="runtime-detail-heading"><strong>{{ row.task_type }}</strong><el-tag type="info" size="small">{{ executionBoundaryText(row.execution_boundary) }}</el-tag></div>
            <dl class="detail-values">
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.backlog') }}</dt>
                <dd>{{ row.pending_count }} / {{ row.running_count }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.throughput') }}</dt>
                <dd>{{ formatDecimal(row.throughput_per_hour) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.failure_rate') }}</dt>
                <dd>{{ formatPercentage(row.failure_rate) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.queue_duration') }}</dt>
                <dd>{{ formatDuration(row.avg_queue_duration_ms) }} / {{ formatDuration(row.p95_queue_duration_ms) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.execution_duration') }}</dt>
                <dd>{{ formatDuration(row.avg_execution_duration_ms) }} / {{ formatDuration(row.p95_execution_duration_ms) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.automatic_retry') }}</dt>
                <dd>{{ formatCountRate(row.automatic_retry_count, row.automatic_retry_rate) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.user_retry') }}</dt>
                <dd>{{ formatCountRate(row.user_retry_count, row.user_retry_rate) }}</dd>
              </div>
              <div>
                <dt>{{ t('monitor.dashboard.runtime_metrics.recovery') }}</dt>
                <dd>{{ formatCountRate(row.recovery_count, row.recovery_rate) }}</dd>
              </div>
            </dl>
          </section>
        </div>
      </div>
    </el-drawer>

    <!-- 最近执行记录 -->
    <el-card v-if="canReadExecutions" shadow="hover" style="margin-top: 20px;">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.dashboard.recent_executions') }}</span>
          <el-button v-if="authStore.hasPermission('monitor.execution.read')" text @click="gotoExecutionList">
            {{ t('monitor.dashboard.view_all') }}
            <el-icon><ArrowRight /></el-icon>
          </el-button>
        </div>
      </template>
      <execution-table
        :executions="recentExecutions"
        :can-view="authStore.hasPermission('monitor.execution.read')"
        @view="handleViewExecution"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, computed, watch, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts'
import { ElMessage } from 'element-plus'
import {
  getStatistics,
  getTrendData,
  listExecutions,
  checkAllProviderHealth,
  listRuntimeHealth,
  getExecutionRuntimeMetrics
} from '@/api/monitor'
import StatisticsCard from '@/components/StatisticsCard.vue'
import ExecutionTable from '@/components/ExecutionTable.vue'
import { useTheme } from '@common-ui'
import { executionDetailLocation } from '@/utils/executionNavigation'
import { navigateMonitorRoute } from '@/utils/moduleNavigation'
import { useAuthStore } from '../store/auth'

const router = useRouter()
const authStore = useAuthStore()
const { t, te } = useI18n()
const { mode } = useTheme()
const canReadHealth = computed(() => authStore.hasPermission('monitor.health.read'))
const canReadExecutions = computed(() => authStore.hasPermission('monitor.execution.read'))

// 数据
const stats = ref({})
const modules = ref([])
const trendData = ref([])
const recentExecutions = ref([])
const runtimeHealth = ref([])
const runtimeMetrics = ref([])
const trendDays = ref(7)
const runtimeMetricsDuration = ref('24h')
const detailModule = ref(null)
const modulesError = ref(false)
const runtimeHealthError = ref(false)
const runtimeMetricsError = ref(false)
let runtimeMetricsRequest = 0

const moduleCards = computed(() => {
  const cards = new Map()
  function get(module) {
    if (!cards.has(module)) cards.set(module, { module, provider: null, runtimes: [], metrics: [] })
    return cards.get(module)
  }
  if (canReadHealth.value) {
    for (const provider of modules.value) get(provider.module).provider = provider
    for (const runtime of runtimeHealth.value) get(runtime.module).runtimes.push(runtime)
  }
  for (const metric of runtimeMetrics.value) get(metric.module).metrics.push(metric)
  return [...cards.values()].sort((a, b) => a.module.localeCompare(b.module)).map(card => {
    const sum = (rows, key) => rows.reduce((total, row) => total + row[key], 0)
    const completed = sum(card.metrics, 'completed_count')
    const pending = sum(card.metrics, 'pending_count')
    return {
      ...card, completed, pending,
      latency: Math.max(0, ...(card.provider?.backends || []).map(item => item.module_health?.latency || 0)),
      running: sum(card.metrics, 'running_count'), throughput: sum(card.metrics, 'throughput_per_hour'),
      online: sum(card.runtimes, 'healthy_instances'), active: sum(card.runtimes, 'active_count'),
      capacity: sum(card.runtimes, 'capacity'),
      failureRate: completed ? 100 * (sum(card.metrics, 'failed_count') + sum(card.metrics, 'timeout_count')) / completed : 0
    }
  })
})

const detailCard = computed(() => moduleCards.value.find(card => card.module === detailModule.value))

function providerTagType(status) {
  return { up: 'success', degraded: 'warning', down: 'danger' }[status] || 'info'
}

function providerStatusText(status) {
  const key = `monitor.module_status.${status}`
  return te(key) ? t(key) : t('monitor.module_status.unknown')
}

// 加载状态
const loadingModules = ref(false)
const loadingTrend = ref(false)
const loadingRuntimeHealth = ref(false)
const loadingRuntimeMetrics = ref(false)

// 图表实例
const trendChart = ref(null)
let chartInstance = null
let trendResizeObserver = null
let refreshTimer = null
let runtimeMetricsRefreshTimer = null

// 计算 echarts 主题
const echartsTheme = computed(() => {
  return mode.value === 'dark' || mode.value === 'blue' ? 'dark' : null
})

// 加载统计数据
async function loadStatistics() {
  try {
    const data = await getStatistics({ duration: '24h' })
    stats.value = data
  } catch (error) {
    ElMessage.error(t('monitor.dashboard.stats_failed'))
    console.error(error)
  }
}

// 加载模块健康状态
async function loadModulesHealth() {
  if (!canReadHealth.value) return
  loadingModules.value = true
  try {
    const data = await checkAllProviderHealth()
    if (canReadHealth.value) {
      modules.value = data || []
      modulesError.value = false
    }
  } catch (error) {
    if (canReadHealth.value) {
      modules.value = []
      modulesError.value = true
      ElMessage.error(t('monitor.dashboard.modules_failed'))
      console.error(error)
    }
  } finally {
    loadingModules.value = false
  }
}

async function loadRuntimeHealth(options = {}) {
  if (!canReadHealth.value) return
  const silent = options.silent === true
  if (!silent) loadingRuntimeHealth.value = true
  try {
    const result = await listRuntimeHealth()
    if (canReadHealth.value) {
      runtimeHealth.value = result || []
      runtimeHealthError.value = false
    }
  } catch (error) {
    if (canReadHealth.value) {
      runtimeHealth.value = []
      runtimeHealthError.value = true
      if (!silent) ElMessage.error(t('monitor.dashboard.runtime_health.load_failed'))
      console.error(error)
    }
  } finally {
    if (!silent) loadingRuntimeHealth.value = false
  }
}

async function loadRuntimeMetrics(options = {}) {
  const request = ++runtimeMetricsRequest
  const silent = options.silent === true
  if (!silent) {
    loadingRuntimeMetrics.value = true
    runtimeMetrics.value = []
  }
  try {
    const result = await getExecutionRuntimeMetrics({ duration: runtimeMetricsDuration.value })
    if (request !== runtimeMetricsRequest) return
    runtimeMetrics.value = result.groups || []
    runtimeMetricsError.value = false
  } catch (error) {
    if (request !== runtimeMetricsRequest) return
    runtimeMetrics.value = []
    runtimeMetricsError.value = true
    if (!silent) ElMessage.error(t('monitor.dashboard.runtime_metrics.load_failed'))
    console.error(error)
  } finally {
    if (request === runtimeMetricsRequest) loadingRuntimeMetrics.value = false
  }
}

async function refreshOverview() {
  await Promise.all([loadModulesHealth(), loadRuntimeHealth(), loadRuntimeMetrics()])
}

function executionBoundaryText(boundary) {
  if (!boundary) return '-'
  const key = `monitor.dashboard.runtime_metrics.boundaries.${boundary}`
  return te(key) ? t(key) : boundary
}

function formatDecimal(value) {
  return Number(value || 0).toFixed(2)
}

function formatPercentage(value) {
  return `${formatDecimal(value)}%`
}

function formatCountRate(count, rate) {
  return `${count || 0} (${formatPercentage(rate)})`
}

function formatDuration(value) {
  const milliseconds = Number(value || 0)
  if (milliseconds < 1000) return `${milliseconds.toFixed(0)} ms`
  if (milliseconds < 60000) return `${(milliseconds / 1000).toFixed(1)} s`
  return `${(milliseconds / 60000).toFixed(1)} min`
}

function runtimeRoleText(role) {
  if (!role) return '-'
  const key = `monitor.dashboard.runtime_health.roles.${role}`
  return te(key) ? t(key) : role
}

function runtimeStatusText(status) {
  if (!status) return '-'
  const key = `monitor.dashboard.runtime_health.statuses.${status}`
  return te(key) ? t(key) : status
}

function runtimeStatusTagType(status) {
  if (status === 'up') return 'success'
  if (status === 'down') return 'danger'
  return 'info'
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString()
}

// 加载趋势数据
async function loadTrendData() {
  loadingTrend.value = true
  try {
    const data = await getTrendData({ days: trendDays.value })
    trendData.value = data || []
    await nextTick()
    renderTrendChart()
  } catch (error) {
    ElMessage.error(t('monitor.dashboard.trend_failed'))
    console.error(error)
  } finally {
    loadingTrend.value = false
  }
}

// 渲染趋势图表
function renderTrendChart() {
  if (!trendChart.value) return

  // 如果已存在图表实例，先销毁再重新创建（以支持主题切换）
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }

  // 使用当前主题初始化图表
  chartInstance = echarts.init(trendChart.value, echartsTheme.value)

  // 从 CSS 变量获取颜色
  const getCssVar = (varName) => {
    const value = getComputedStyle(document.documentElement).getPropertyValue(varName).trim()
    if (!value) {
      console.warn(`CSS variable ${varName} not found, theme system may not be loaded properly`)
    }
    return value
  }

  const successColor = getCssVar('--el-color-success')
  const dangerColor = getCssVar('--el-color-danger')
  const primaryColor = getCssVar('--el-color-primary')
  const textColor = getCssVar('--addp-text-primary')
  const textSecondaryColor = getCssVar('--addp-text-secondary')
  const borderColor = getCssVar('--addp-border-color')

  const option = {
    // 全局文字样式
    textStyle: {
      color: textColor
    },
    tooltip: {
      trigger: 'axis',
      renderMode: 'html',
      backgroundColor: 'var(--addp-bg-primary)',
      borderColor: 'var(--addp-border-color)',
      textStyle: {
        color: 'var(--addp-text-primary)'
      }
    },
    legend: {
      data: [t('monitor.dashboard.chart.success'), t('monitor.dashboard.chart.failed'), t('monitor.dashboard.chart.total')],
      textStyle: {
        color: textColor
      }
    },
    xAxis: {
      type: 'category',
      data: trendData.value.map(d => d.date),
      axisLabel: {
        color: textSecondaryColor
      },
      axisLine: {
        lineStyle: {
          color: textSecondaryColor
        }
      }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        color: textSecondaryColor
      },
      axisLine: {
        lineStyle: {
          color: textSecondaryColor
        }
      },
      splitLine: {
        lineStyle: {
          color: borderColor
        }
      }
    },
    series: [
      {
        name: t('monitor.dashboard.chart.success'),
        type: 'line',
        data: trendData.value.map(d => d.success_count),
        smooth: true,
        itemStyle: { color: successColor }
      },
      {
        name: t('monitor.dashboard.chart.failed'),
        type: 'line',
        data: trendData.value.map(d => d.failed_count),
        smooth: true,
        itemStyle: { color: dangerColor }
      },
      {
        name: t('monitor.dashboard.chart.total'),
        type: 'line',
        data: trendData.value.map(d => d.total),
        smooth: true,
        itemStyle: { color: primaryColor }
      }
    ]
  }

  chartInstance.setOption(option)
}

// 加载最近执行记录
async function loadRecentExecutions() {
  if (!canReadExecutions.value) return
  try {
    const data = await listExecutions({ page: 1, page_size: 10 })
    if (canReadExecutions.value) recentExecutions.value = data.executions || []
  } catch (error) {
    if (canReadExecutions.value) {
      ElMessage.error(t('monitor.dashboard.executions_failed'))
      console.error(error)
    }
  }
}

async function refreshExecutionSummary() {
  await Promise.all([
    loadStatistics(),
    canReadExecutions.value ? loadRecentExecutions() : Promise.resolve(),
    canReadHealth.value ? loadRuntimeHealth({ silent: true }) : Promise.resolve()
  ])
}

// 查看执行详情
function handleViewExecution(row) {
  const location = executionDetailLocation(row)
  if (location) {
    navigateMonitorRoute(router, location)
  }
}

// 跳转到执行列表页
function gotoExecutionList() {
  navigateMonitorRoute(router, '/executions')
}

// 初始化
onMounted(async () => {
  trendResizeObserver = new ResizeObserver(() => chartInstance?.resize())
  trendResizeObserver.observe(trendChart.value)
  await Promise.all([
    loadStatistics(),
    canReadHealth.value ? loadModulesHealth() : Promise.resolve(),
    loadTrendData(),
    canReadExecutions.value ? loadRecentExecutions() : Promise.resolve(),
    canReadHealth.value ? loadRuntimeHealth() : Promise.resolve(),
    loadRuntimeMetrics()
  ])
  refreshTimer = window.setInterval(refreshExecutionSummary, 5000)
  runtimeMetricsRefreshTimer = window.setInterval(() => loadRuntimeMetrics({ silent: true }), 30000)
})

watch(canReadHealth, allowed => {
  if (allowed) {
    void Promise.all([loadModulesHealth(), loadRuntimeHealth()])
  } else {
    modules.value = []
    runtimeHealth.value = []
    modulesError.value = false
    runtimeHealthError.value = false
  }
})

watch(canReadExecutions, allowed => {
  if (allowed) {
    void loadRecentExecutions()
  } else {
    recentExecutions.value = []
  }
})

// 监听主题变化，重新渲染图表
watch(echartsTheme, () => {
  if (trendData.value.length > 0) {
    renderTrendChart()
  }
})

// 清理图表实例
onBeforeUnmount(() => {
  runtimeMetricsRequest++
  trendResizeObserver?.disconnect()
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (runtimeMetricsRefreshTimer) {
    window.clearInterval(runtimeMetricsRefreshTimer)
    runtimeMetricsRefreshTimer = null
  }
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
})
</script>

<style scoped>
.dashboard {
  padding: 20px;
  background: var(--addp-bg-secondary);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header span {
  color: var(--addp-text-primary);
  font-weight: 500;
  font-size: 16px;
}

.trend-chart {
  width: 100%;
  height: 300px;
}

.runtime-overview {
  margin-top: 20px;
}

.overview-header, .runtime-metrics-actions {
  flex-wrap: wrap;
  gap: 12px;
}

.overview-errors {
  display: grid;
  gap: 8px;
}

.overview-errors:not(:empty) {
  margin-bottom: 16px;
}

.module-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: 16px;
}

.module-runtime-card {
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  overflow-wrap: anywhere;
}

.module-card-heading, .runtime-detail-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.module-card-heading h3 {
  margin: 0;
  color: var(--addp-text-primary);
  font-size: 16px;
}

.module-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(125px, 1fr));
  gap: 16px;
  margin: 16px 0 0;
}

.module-summary dt, .detail-values dt {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.module-summary dd {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 6px 0 0;
  color: var(--addp-text-primary);
  font-size: 18px;
  font-weight: 600;
}

.backend-count {
  font-size: 14px;
  white-space: nowrap;
}

.module-details h4 {
  margin: 16px 0 12px;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.runtime-detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 310px), 1fr));
  gap: 12px;
}

.runtime-detail {
  min-width: 0;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
  padding: 12px;
}

.runtime-detail-heading {
  color: var(--addp-text-primary);
}

.detail-values {
  margin: 12px 0 0;
  display: grid;
  gap: 10px;
}

.detail-values > div {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.detail-values dd {
  margin: 0;
  text-align: right;
  color: var(--addp-text-primary);
  font-size: 13px;
}



.card-description {
  margin-top: 4px;
  color: var(--addp-text-secondary);
  font-size: 12px;
  font-weight: 400;
}

.runtime-metrics-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
