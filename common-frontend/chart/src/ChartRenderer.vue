<template>
  <div ref="element" class="chart-renderer" />
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
const element = ref(null)
let chart
let resizeObserver

async function render() {
  const validation = validateChartResult(props.rows, props.config, props.hasMore)
  if (!validation.valid) {
    chart?.clear()
    emit('invalid', validation.reason)
    return
  }
  await nextTick()
  if (!element.value) return
  if (!chart) {
    chart = echarts.init(element.value)
    chart.on('click', (event) => {
      const selection = resultSelectionFromChartEvent(event, props.rows.length)
      if (selection) emit('result-select', selection)
    })
  }
  chart.setOption(buildChartOption(props.rows, props.config), true)
}

onMounted(() => {
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(element.value)
  render()
})
watch(() => [props.rows, props.config, props.hasMore], render, { deep: true })
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  chart?.dispose()
})
</script>

<style scoped>
.chart-renderer { width: 100%; min-height: 420px; }
</style>
