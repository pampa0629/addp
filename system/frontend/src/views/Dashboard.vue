<template>
  <Layout>
    <!-- 统计卡片 -->
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card shadow="hover">
          <el-statistic title="今日日志" :value="stats.todayCount" :loading="loading">
            <template #prefix>
              <el-icon><Document /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <el-statistic title="7日日志" :value="stats.weekCount" :loading="loading">
            <template #prefix>
              <el-icon><Files /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <el-statistic title="30日日志" :value="stats.monthCount" :loading="loading">
            <template #prefix>
              <el-icon><Grid /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <el-statistic title="今日错误" :value="stats.errorCount" :loading="loading" :value-style="{ color: stats.errorCount > 0 ? 'var(--el-color-danger)' : 'var(--el-color-success)' }">
            <template #prefix>
              <el-icon><Monitor /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
    </el-row>

    <!-- 图表区域 -->
    <el-row :gutter="20" style="margin-top: 20px">
      <!-- 时间趋势图 -->
      <el-col :span="24">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>日志趋势</span>
              <el-radio-group v-model="trendType" size="small" @change="loadTrends">
                <el-radio-button label="daily">30天趋势</el-radio-button>
                <el-radio-button label="hourly">24小时趋势</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <v-chart :option="trendChartOption" style="height: 300px" :loading="trendsLoading" />
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px">
      <!-- 用户行为分析 -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>活跃用户 TOP5（7日）</span>
            </div>
          </template>
          <v-chart :option="userChartOption" style="height: 300px" :loading="loading" />
        </el-card>
      </el-col>

      <!-- 模块使用情况 -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>模块使用情况（7日）</span>
            </div>
          </template>
          <v-chart :option="moduleChartOption" style="height: 300px" :loading="loading" />
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px">
      <!-- 操作类型分布 -->
      <el-col :span="24">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>操作类型分布（7日）</span>
            </div>
          </template>
          <v-chart :option="actionChartOption" style="height: 300px" :loading="loading" />
        </el-card>
      </el-col>
    </el-row>
  </Layout>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import Layout from '../components/Layout.vue'
import { Document, Files, Grid, Monitor } from '@element-plus/icons-vue'
import { logsAPI } from '../api/logs'

// 数据状态
const loading = ref(false)
const trendsLoading = ref(false)
const trendType = ref('daily')
const stats = ref({
  todayCount: 0,
  weekCount: 0,
  monthCount: 0,
  errorCount: 0,
  topUsers: [],
  topModules: [],
  actionDistribution: []
})
const trends = ref({
  daily: [],
  hourly: []
})

// 加载统计数据
const loadStats = async () => {
  loading.value = true
  try {
    const response = await logsAPI.getStats()
    stats.value = response
  } catch (error) {
    console.error('加载统计数据失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载趋势数据
const loadTrends = async () => {
  trendsLoading.value = true
  try {
    const response = await logsAPI.getTrends()
    trends.value = response
  } catch (error) {
    console.error('加载趋势数据失败:', error)
  } finally {
    trendsLoading.value = false
  }
}

// 时间趋势图配置
const trendChartOption = computed(() => {
  const data = trendType.value === 'daily' ? trends.value.daily : trends.value.hourly
  return {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross' }
    },
    grid: {
      left: '3%',
      right: '4%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: data.map(d => {
        if (trendType.value === 'daily') {
          // 格式化日期：只显示月-日
          return d.timestamp.substring(5, 10)
        } else {
          // 格式化小时：只显示日期+小时
          return d.timestamp.substring(5, 13)
        }
      })
    },
    yAxis: {
      type: 'value',
      name: '日志数量'
    },
    series: [{
      type: 'line',
      data: data.map(d => d.count),
      smooth: true,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(64, 158, 255, 0.3)' },
            { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
          ]
        }
      },
      lineStyle: {
        color: 'var(--el-color-primary)',
        width: 2
      },
      itemStyle: {
        color: 'var(--el-color-primary)'
      }
    }]
  }
})

// 用户行为分析图配置
const userChartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    axisPointer: { type: 'shadow' }
  },
  grid: {
    left: '3%',
    right: '4%',
    bottom: '3%',
    containLabel: true
  },
  xAxis: {
    type: 'category',
    data: stats.value.topUsers.map(u => u.username)
  },
  yAxis: {
    type: 'value',
    name: '操作次数'
  },
  series: [{
    type: 'bar',
    data: stats.value.topUsers.map(u => u.count),
    itemStyle: {
      color: {
        type: 'linear',
        x: 0,
        y: 0,
        x2: 0,
        y2: 1,
        colorStops: [
          { offset: 0, color: 'var(--el-color-success)' },
          { offset: 1, color: '#85CE61' }
        ]
      }
    }
  }]
}))

// 模块使用情况图配置
const moduleChartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    axisPointer: { type: 'shadow' }
  },
  grid: {
    left: '3%',
    right: '4%',
    bottom: '3%',
    containLabel: true
  },
  xAxis: {
    type: 'category',
    data: stats.value.topModules.map(m => m.module_name)
  },
  yAxis: {
    type: 'value',
    name: '日志数量'
  },
  series: [{
    type: 'bar',
    data: stats.value.topModules.map(m => m.count),
    itemStyle: {
      color: {
        type: 'linear',
        x: 0,
        y: 0,
        x2: 0,
        y2: 1,
        colorStops: [
          { offset: 0, color: 'var(--el-color-primary)' },
          { offset: 1, color: '#66B1FF' }
        ]
      }
    }
  }]
}))

// 操作类型分布图配置
const actionChartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    axisPointer: { type: 'shadow' }
  },
  grid: {
    left: '3%',
    right: '4%',
    bottom: '3%',
    containLabel: true
  },
  xAxis: {
    type: 'category',
    data: stats.value.actionDistribution.map(a => a.action_type)
  },
  yAxis: {
    type: 'value',
    name: '操作次数'
  },
  series: [{
    type: 'bar',
    data: stats.value.actionDistribution.map(a => a.count),
    itemStyle: {
      color: function(params) {
        const colors = ['var(--el-color-warning)', 'var(--el-color-danger)', 'var(--addp-text-tertiary)', 'var(--el-color-success)']
        return colors[params.dataIndex % colors.length]
      }
    }
  }]
}))

// 组件挂载时加载数据
onMounted(() => {
  loadStats()
  loadTrends()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}
</style>
