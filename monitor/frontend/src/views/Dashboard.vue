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

    <el-row :gutter="20">
      <!-- 模块健康状态 -->
      <el-col :span="8">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>{{ t('monitor.dashboard.module_health') }}</span>
              <el-button text @click="refreshModulesHealth">
                <el-icon><Refresh /></el-icon>
                {{ t('monitor.dashboard.refresh') }}
              </el-button>
            </div>
          </template>
          <div v-loading="loadingModules">
            <module-status-badge
              v-for="module in modules"
              :key="module.module"
              :module="module"
            />
            <el-empty v-if="modules.length === 0" :description="t('monitor.dashboard.no_modules')" />
          </div>
        </el-card>
      </el-col>

      <!-- 执行趋势图表 -->
      <el-col :span="16">
        <el-card shadow="hover">
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
          <div ref="trendChart" style="width: 100%; height: 300px;" v-loading="loadingTrend"></div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="hover" class="runtime-health-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.dashboard.runtime_health.title') }}</span>
          <el-button text @click="refreshRuntimeHealth">
            <el-icon><Refresh /></el-icon>
            {{ t('monitor.dashboard.refresh') }}
          </el-button>
        </div>
      </template>
      <el-table :data="runtimeHealth" v-loading="loadingRuntimeHealth" size="small">
        <el-table-column prop="module" :label="t('monitor.table.module')" width="120" />
        <el-table-column :label="t('monitor.dashboard.runtime_health.role')" min-width="160">
          <template #default="{ row }">{{ runtimeRoleText(row.role) }}</template>
        </el-table-column>
        <el-table-column prop="runtime_name" :label="t('monitor.dashboard.runtime_health.runtime')" min-width="150" />
        <el-table-column :label="t('monitor.table.status')" width="110">
          <template #default="{ row }">
            <el-tag :type="runtimeStatusTagType(row.status)" size="small">{{ runtimeStatusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_health.instances')" width="120">
          <template #default="{ row }">{{ row.healthy_instances }} / {{ row.known_instances }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_health.load')" width="120">
          <template #default="{ row }">{{ row.active_count }} / {{ row.capacity }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_health.last_heartbeat')" min-width="180">
          <template #default="{ row }">{{ formatDate(row.last_heartbeat_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_health.expires_at')" min-width="180">
          <template #default="{ row }">{{ formatDate(row.expires_at) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loadingRuntimeHealth && runtimeHealth.length === 0" :description="t('monitor.dashboard.runtime_health.empty')" />
    </el-card>

    <el-card shadow="hover" class="runtime-metrics-card">
      <template #header>
        <div class="card-header">
          <div>
            <span>{{ t('monitor.dashboard.runtime_metrics.title') }}</span>
            <div class="card-description">{{ t('monitor.dashboard.runtime_metrics.description') }}</div>
          </div>
          <div class="runtime-metrics-actions">
            <el-select v-model="runtimeMetricsDuration" style="width: 110px;" @change="loadRuntimeMetrics">
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.24h')" value="24h" />
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.7d')" value="7d" />
              <el-option :label="t('monitor.dashboard.runtime_metrics.windows.30d')" value="30d" />
            </el-select>
            <el-button text @click="refreshRuntimeMetrics">
              <el-icon><Refresh /></el-icon>
              {{ t('monitor.dashboard.refresh') }}
            </el-button>
          </div>
        </div>
      </template>
      <el-table :data="runtimeMetrics" v-loading="loadingRuntimeMetrics" size="small">
        <el-table-column prop="module" :label="t('monitor.table.module')" width="110" fixed />
        <el-table-column prop="task_type" :label="t('monitor.table.task_type')" min-width="160" fixed />
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.boundary')" width="115">
          <template #default="{ row }">{{ executionBoundaryText(row.execution_boundary) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.backlog')" width="125">
          <template #default="{ row }">{{ row.pending_count }} / {{ row.running_count }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.throughput')" width="120">
          <template #default="{ row }">{{ formatDecimal(row.throughput_per_hour) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.queue_duration')" min-width="160">
          <template #default="{ row }">
            {{ formatDuration(row.avg_queue_duration_ms) }} / {{ formatDuration(row.p95_queue_duration_ms) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.execution_duration')" min-width="160">
          <template #default="{ row }">
            {{ formatDuration(row.avg_execution_duration_ms) }} / {{ formatDuration(row.p95_execution_duration_ms) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.failure_rate')" width="110">
          <template #default="{ row }">{{ formatPercentage(row.failure_rate) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.automatic_retry')" min-width="125">
          <template #default="{ row }">{{ formatCountRate(row.automatic_retry_count, row.automatic_retry_rate) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.user_retry')" min-width="125">
          <template #default="{ row }">{{ formatCountRate(row.user_retry_count, row.user_retry_rate) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.dashboard.runtime_metrics.recovery')" min-width="125">
          <template #default="{ row }">{{ formatCountRate(row.recovery_count, row.recovery_rate) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loadingRuntimeMetrics && runtimeMetrics.length === 0" :description="t('monitor.dashboard.runtime_metrics.empty')" />
    </el-card>

    <!-- 最近执行记录 -->
    <el-card shadow="hover" style="margin-top: 20px;">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.dashboard.recent_executions') }}</span>
          <el-button text @click="gotoExecutionList">
            {{ t('monitor.dashboard.view_all') }}
            <el-icon><ArrowRight /></el-icon>
          </el-button>
        </div>
      </template>
      <execution-table
        :executions="recentExecutions"
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
import ModuleStatusBadge from '@/components/ModuleStatusBadge.vue'
import ExecutionTable from '@/components/ExecutionTable.vue'
import { useTheme } from '@common-ui'
import { executionDetailLocation } from '@/utils/executionNavigation'
import { navigateMonitorRoute } from '@/utils/moduleNavigation'

const router = useRouter()
const { t, te } = useI18n()
const { mode } = useTheme()

// 数据
const stats = ref({})
const modules = ref([])
const trendData = ref([])
const recentExecutions = ref([])
const runtimeHealth = ref([])
const runtimeMetrics = ref([])
const trendDays = ref(7)
const runtimeMetricsDuration = ref('24h')

// 加载状态
const loadingModules = ref(false)
const loadingTrend = ref(false)
const loadingRuntimeHealth = ref(false)
const loadingRuntimeMetrics = ref(false)

// 图表实例
const trendChart = ref(null)
let chartInstance = null
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
  loadingModules.value = true
  try {
    const data = await checkAllProviderHealth()
    modules.value = data || []
  } catch (error) {
    ElMessage.error(t('monitor.dashboard.modules_failed'))
    console.error(error)
  } finally {
    loadingModules.value = false
  }
}

// 刷新模块健康状态
async function refreshModulesHealth() {
  await loadModulesHealth()
  ElMessage.success(t('monitor.dashboard.refresh_success'))
}

async function loadRuntimeHealth(options = {}) {
  const silent = options.silent === true
  if (!silent) loadingRuntimeHealth.value = true
  try {
    runtimeHealth.value = await listRuntimeHealth() || []
  } catch (error) {
    if (!silent) ElMessage.error(t('monitor.dashboard.runtime_health.load_failed'))
    console.error(error)
  } finally {
    if (!silent) loadingRuntimeHealth.value = false
  }
}

async function refreshRuntimeHealth() {
  await loadRuntimeHealth()
  ElMessage.success(t('monitor.dashboard.refresh_success'))
}

async function loadRuntimeMetrics(options = {}) {
  const silent = options.silent === true
  if (!silent) loadingRuntimeMetrics.value = true
  try {
    const result = await getExecutionRuntimeMetrics({ duration: runtimeMetricsDuration.value })
    runtimeMetrics.value = result.groups || []
  } catch (error) {
    if (!silent) ElMessage.error(t('monitor.dashboard.runtime_metrics.load_failed'))
    console.error(error)
  } finally {
    if (!silent) loadingRuntimeMetrics.value = false
  }
}

async function refreshRuntimeMetrics() {
  await loadRuntimeMetrics()
  ElMessage.success(t('monitor.dashboard.refresh_success'))
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
      textStyle: {
        color: textColor
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
  try {
    const data = await listExecutions({ page: 1, page_size: 10 })
    recentExecutions.value = data.executions || []
  } catch (error) {
    ElMessage.error(t('monitor.dashboard.executions_failed'))
    console.error(error)
  }
}

async function refreshExecutionSummary() {
  await Promise.all([
    loadStatistics(),
    loadRecentExecutions(),
    loadRuntimeHealth({ silent: true })
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
  await Promise.all([
    loadStatistics(),
    loadModulesHealth(),
    loadTrendData(),
    loadRecentExecutions(),
    loadRuntimeHealth(),
    loadRuntimeMetrics()
  ])
  refreshTimer = window.setInterval(refreshExecutionSummary, 5000)
  runtimeMetricsRefreshTimer = window.setInterval(() => loadRuntimeMetrics({ silent: true }), 30000)
})

// 监听主题变化，重新渲染图表
watch(echartsTheme, () => {
  if (trendData.value.length > 0) {
    renderTrendChart()
  }
})

// 清理图表实例
onBeforeUnmount(() => {
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

.runtime-health-card {
  margin-top: 20px;
}

.runtime-metrics-card {
  margin-top: 20px;
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
