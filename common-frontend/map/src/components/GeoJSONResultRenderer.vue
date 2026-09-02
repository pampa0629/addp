<template>
  <div class="geojson-result-renderer">
    <MapContainer
      :features="features"
      :base-map-type="baseMapType"
      :height="height"
      :popup-options="{ fields: config.tooltip_fields || [], primaryField: config.label_field || '' }"
      :feature-style="featureStyle"
      @feature-click="selectFeature"
      @view-state-change="$emit('view-state-change', $event)"
    />
    <aside v-if="legendEntries.length > 0" class="map-legend" :aria-label="config.style?.legend_title || config.style?.field">
      <strong>{{ config.style?.legend_title || config.style?.field }}</strong>
      <div v-for="entry in legendEntries" :key="entry.label" class="legend-entry">
        <span class="legend-color" :style="{ backgroundColor: `var(${thematicColorVariable(entry.index, entry.count, config.style?.palette)})` }" />
        <span>{{ entry.label }}</span>
      </div>
    </aside>
  </div>
</template>

<script setup>
import { computed, watchEffect } from 'vue'
import MapContainer from './map/MapContainer.vue'
import { crsSuppressionStatus, getPreviewCRSTransform, transformGeoJSONGeometryToWGS84 } from '../utils/crsRegistry'
import { buildGeoJSONFeatures, resultSelectionFromFeature, spatialPreviewDescriptor, validateGeoJSONResult } from '../utils/geoJSONResult.mjs'
import { buildThematicContext, thematicColorVariable } from '../utils/thematicMap.mjs'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  config: { type: Object, required: true },
  spatial: { type: Object, required: true },
  hasMore: { type: Boolean, default: false },
  baseMapType: { type: String, default: 'osm' },
  height: { type: String, default: '420px' }
})
const emit = defineEmits(['invalid', 'result-select', 'view-state-change'])
const transform = computed(() => getPreviewCRSTransform(spatialPreviewDescriptor(props.spatial, props.config.geometry_field)))
const resultValidation = computed(() => validateGeoJSONResult(props.rows, props.hasMore))
const features = computed(() => {
  if (!resultValidation.value.valid || crsSuppressionStatus(transform.value)) return []
  return buildGeoJSONFeatures(
    props.rows,
    props.config,
    (geometry) => transformGeoJSONGeometryToWGS84(geometry, transform.value)
  )
})
const featureStyle = computed(() => buildThematicContext(features.value, props.config.style || { mode: 'uniform', palette: 'primary' }))
const legendEntries = computed(() => featureStyle.value.valid ? featureStyle.value.entries || [] : [])
const validation = computed(() => resultValidation.value.valid ? featureStyle.value : resultValidation.value)

function selectFeature(event) {
  const selection = resultSelectionFromFeature(event?.feature, props.rows.length)
  if (selection) emit('result-select', selection)
}

watchEffect(() => {
  if (!validation.value.valid) emit('invalid', validation.value.reason)
  else if (crsSuppressionStatus(transform.value)) emit('invalid', crsSuppressionStatus(transform.value))
})
</script>

<style scoped>
.geojson-result-renderer {
  position: relative;
}

.map-legend {
  position: absolute;
  right: 16px;
  bottom: 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: min(240px, calc(100% - 32px));
  padding: 12px;
  overflow: hidden;
  color: var(--addp-text-primary);
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  box-shadow: var(--addp-shadow-card);
}

.map-legend strong,
.legend-entry span:last-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.legend-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 12px;
}

.legend-color {
  flex: 0 0 16px;
  width: 16px;
  height: 10px;
  border: 1px solid var(--addp-border-color);
  border-radius: 2px;
}
</style>
