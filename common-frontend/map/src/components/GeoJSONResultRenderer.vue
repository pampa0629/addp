<template>
  <MapContainer
    :features="features"
    :base-map-type="baseMapType"
    :height="height"
    :popup-options="{ fields: config.tooltip_fields || [] }"
    @feature-click="selectFeature"
    @view-state-change="$emit('view-state-change', $event)"
  />
</template>

<script setup>
import { computed, watchEffect } from 'vue'
import MapContainer from './map/MapContainer.vue'
import { crsSuppressionStatus, getPreviewCRSTransform, transformGeoJSONGeometryToWGS84 } from '../utils/crsRegistry'
import { buildGeoJSONFeatures, resultSelectionFromFeature, spatialPreviewDescriptor, validateGeoJSONResult } from '../utils/geoJSONResult.mjs'

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
const validation = computed(() => validateGeoJSONResult(props.rows, props.hasMore))
const features = computed(() => {
  if (!validation.value.valid || crsSuppressionStatus(transform.value)) return []
  return buildGeoJSONFeatures(
    props.rows,
    props.config,
    (geometry) => transformGeoJSONGeometryToWGS84(geometry, transform.value)
  )
})

function selectFeature(event) {
  const selection = resultSelectionFromFeature(event?.feature, props.rows.length)
  if (selection) emit('result-select', selection)
}

watchEffect(() => {
  if (!validation.value.valid) emit('invalid', validation.value.reason)
  else if (crsSuppressionStatus(transform.value)) emit('invalid', crsSuppressionStatus(transform.value))
})
</script>
