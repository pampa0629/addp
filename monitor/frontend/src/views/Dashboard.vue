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
  checkAllModules
} from '@/api/monitor'
import StatisticsCard from '@/components/StatisticsCard.vue'
import ModuleStatusBadge from '@/components/ModuleStatusBadge.vue'
import ExecutionTable from '@/components/ExecutionTable.vue'
import { useTheme } from '@common-ui'

const router = useRouter()
const { t } = useI18n()
const { mode } = useTheme()

// 数据
const stats = ref({})
const modules = ref([])
const trendData = ref([])
const recentExecutions = ref([])
const trendDays = ref(7)

// 加载状态
const loadingModules = ref(false)
const loadingTrend = ref(false)

// 图表实例
const trendChart = ref(null)
let chartInstance = null

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
    const data = await checkAllModules()
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

// 查看执行详情
function handleViewExecution(row) {
  ElMessage.info(t('monitor.dashboard.view_execution', { id: row.id }))
  // TODO: 实现详情弹窗或跳转
}

// 跳转到执行列表页
function gotoExecutionList() {
  router.push('/executions')
}

// 初始化
onMounted(async () => {
  await Promise.all([
    loadStatistics(),
    loadModulesHealth(),
    loadTrendData(),
    loadRecentExecutions()
  ])
})

// 监听主题变化，重新渲染图表
watch(echartsTheme, () => {
  if (trendData.value.length > 0) {
    renderTrendChart()
  }
})

// 清理图表实例
onBeforeUnmount(() => {
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
})
</script>

<style scoped>
.dashboard {
  padding: 20px;
  min-height: 100vh;
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
</style>
