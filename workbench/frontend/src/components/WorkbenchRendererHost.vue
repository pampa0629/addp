<template>
  <div data-testid="renderer-host">
    <el-empty
      v-if="rendererType === 'value' && !resultReady"
      :description="t('workbench.noData')"
    />
    <TabularResultRenderer
      v-else-if="rendererType === 'table'"
      :rows="rows"
      :columns="config.columns || []"
      :fields="descriptor?.output_contract?.fields || []"
      height="100%"
      @result-select="emit('result-select', $event)"
    />
    <el-alert
      v-else-if="invalidReason"
      type="warning"
      :closable="false"
      :title="t(`workbench.rendererErrors.${invalidReason}`)"
    />
    <ChartRenderer
      v-else-if="rendererType === 'chart'"
      :rows="rows"
      :config="config"
      :has-more="Boolean(page?.has_more)"
      @invalid="invalidReason = $event"
      @result-select="emit('result-select', $event)"
    />
    <ScalarValueRenderer
      v-else-if="rendererType === 'value'"
      :rows="rows"
      :config="config"
      :fields="descriptor?.output_contract?.fields || []"
    />
    <GeoJSONResultRenderer
      v-else-if="rendererType === 'map'"
      :rows="rows"
      :config="config"
      :spatial="descriptor.output_contract.spatial"
      :has-more="Boolean(page?.has_more)"
      @invalid="invalidReason = $event"
      @result-select="emit('result-select', $event)"
    />
  </div>
</template>

<script setup>
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ScalarValueRenderer, TabularResultRenderer } from '@common-ui'
import { validateScalarValueResult } from '@common-ui/utils/scalarValueResult.mjs'
import { validateChartResult } from '@common-ui-chart/chartResult.mjs'
import { validateGeoJSONResult } from '@common-ui-map/utils/geoJSONResult.mjs'

const ChartRenderer = defineAsyncComponent(() => import('@common-ui-chart/ChartRenderer.vue'))
const GeoJSONResultRenderer = defineAsyncComponent(async () => {
  await import('ol/ol.css')
  return import('@common-ui-map/components/GeoJSONResultRenderer.vue')
})

const props = defineProps({
  rows: { type: Array, default: () => [] },
  rendererType: { type: String, required: true },
  config: { type: Object, required: true },
  descriptor: { type: Object, default: null },
  page: { type: Object, default: () => ({}) },
  resultReady: { type: Boolean, default: true }
})
const emit = defineEmits(['result-select'])
const { t } = useI18n()
const emittedReason = ref('')
const validationReason = computed(() => {
  if (props.rendererType === 'chart') {
    return validateChartResult(props.rows, props.config, Boolean(props.page?.has_more)).reason
  }
  if (props.rendererType === 'map') {
    if (!props.descriptor?.output_contract?.spatial) return 'spatial_not_declared'
    return validateGeoJSONResult(props.rows, Boolean(props.page?.has_more)).reason
  }
  if (props.rendererType === 'value') {
    return validateScalarValueResult(props.rows, props.config, Boolean(props.page?.has_more)).reason
  }
  return ''
})
const invalidReason = computed({
  get: () => validationReason.value || emittedReason.value,
  set: (value) => { emittedReason.value = value }
})
watch(() => [props.rows, props.config, props.page], () => { emittedReason.value = '' }, { deep: true })
</script>
