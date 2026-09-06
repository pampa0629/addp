<template>
  <div ref="element" class="chart-renderer" />
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { buildChartOption, resultSelectionFromChartEvent, validateChartResult } from './chartResult.mjs'

echarts.use([BarChart, LineChart, PieChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const props = defineProps({
  rows: { type: Array, default: () => [] },
  config: { type: Object, required: true },
  hasMore: { type: Boolean, default: false }
})
const emit = defineEmits(['invalid', 'result-select'])
const { locale } = useI18n()
const element = ref(null)
let chart = null
let resizeObserver = null

function hasRenderableSize(target) {
  if (!target) return false
  return !(target.clientWidth <= 0 || target.clientHeight <= 0)
}

async function render() {
  const validation = validateChartResult(props.rows, props.config, props.hasMore)
  if (!validation.valid) {
    chart?.clear()
    emit('invalid', validation.reason)
    return
  }
  await nextTick()
  if (!hasRenderableSize(element.value)) return
  if (!chart) {
    chart = echarts.init(element.value)
    chart.on('click', (event) => {
      const selection = resultSelectionFromChartEvent(event, props.rows.length)
      if (selection) emit('result-select', selection)
    })
  }
  chart.setOption(buildChartOption(props.rows, props.config, locale.value), true)
}

onMounted(() => {
  resizeObserver = new ResizeObserver(() => {
    if (!hasRenderableSize(element.value)) return
    if (!chart) {
      render()
      return
    }
    chart.resize()
  })
  resizeObserver.observe(element.value)
  render()
})
watch(() => [props.rows, props.config, props.hasMore, locale.value], render, { deep: true })
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.chart-renderer { width: 100%; min-height: 420px; }
</style>
