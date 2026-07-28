<template>
  <div ref="chartElement" class="profile-chart" />
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([BarChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = defineProps({
  field: {
    type: Object,
    default: null
  },
  nullLabel: {
    type: String,
    default: '-'
  }
})

const chartElement = ref(null)
let chart = null
let resizeObserver = null
let themeObserver = null

const chartSource = computed(() => {
  const distribution = Array.isArray(props.field?.distribution) ? props.field.distribution : []
  if (distribution.length) {
    return {
      horizontal: false,
      labels: distribution.map(bucket => String(bucket.label ?? '')),
      values: distribution.map(bucket => Number(bucket.count || 0))
    }
  }
  const topValues = Array.isArray(props.field?.top_values) ? props.field.top_values : []
  return {
    horizontal: true,
    labels: topValues.map(item => String(item.value ?? props.nullLabel)),
    values: topValues.map(item => Number(item.count || 0))
  }
})

const themeColor = (...names) => {
  if (typeof window === 'undefined') return ''
  const styles = getComputedStyle(document.documentElement)
  return names.map(name => styles.getPropertyValue(name).trim()).find(Boolean) || ''
}

const truncatedLabel = value => {
  const text = String(value ?? '')
  return text.length > 18 ? `${text.slice(0, 18)}...` : text
}

const renderChart = async () => {
  await nextTick()
  if (!chartElement.value) return
  if (!chart) chart = echarts.init(chartElement.value)
  const source = chartSource.value
  const textColor = themeColor('--addp-text-secondary', '--el-text-color-secondary')
  const borderColor = themeColor('--addp-border-color', '--el-border-color')
  const primaryColor = themeColor('--el-color-primary')
  const categoryAxis = {
    type: 'category',
    data: source.labels,
    axisLabel: { color: textColor, formatter: truncatedLabel },
    axisLine: { lineStyle: { color: borderColor } },
    axisTick: { show: false }
  }
  const valueAxis = {
    type: 'value',
    minInterval: 1,
    axisLabel: { color: textColor },
    axisLine: { show: false },
    splitLine: { lineStyle: { color: borderColor, type: 'dashed' } }
  }
  chart.setOption({
    animationDuration: 250,
    color: [primaryColor],
    textStyle: { color: textColor },
    grid: source.horizontal
      ? { top: 12, right: 24, bottom: 24, left: 116, containLabel: false }
      : { top: 16, right: 20, bottom: 54, left: 52 },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: themeColor('--addp-bg-primary', '--el-bg-color'),
      borderColor,
      textStyle: { color: themeColor('--addp-text-primary', '--el-text-color-primary') }
    },
    xAxis: source.horizontal ? valueAxis : categoryAxis,
    yAxis: source.horizontal ? categoryAxis : valueAxis,
    series: [{
      type: 'bar',
      data: source.values,
      barMaxWidth: 32,
      itemStyle: { borderRadius: source.horizontal ? [0, 3, 3, 0] : [3, 3, 0, 0] }
    }]
  }, true)
}

watch(() => props.field, renderChart, { deep: true })

onMounted(() => {
  renderChart()
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(chartElement.value)
  themeObserver = new MutationObserver(renderChart)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  themeObserver?.disconnect()
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.profile-chart {
  width: 100%;
  height: 260px;
  min-height: 260px;
}
</style>
