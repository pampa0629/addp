<template>
  <GridContainerPreview :key="locator" :variables="variables" :load-slice="readSlice">
    <template #message-preview="{ child }">
      <GRIBTemperatureMap
        v-if="gribSourceVersion && child.native?.map_profile === 'ecmwf_ifs_01_temperature/v1'"
        :key="`${locator}:${gribSourceVersion}:${child.name}`"
        :locator="locator" :source-version="gribSourceVersion" :message="child.name"
        :valid-time="child.native.message?.time?.end_utc || ''" />
    </template>
    <template #spatial="{ spatial }">
      <GeoJsonPreview :data="footprintPreview(spatial)" />
    </template>
  </GridContainerPreview>
</template>
<script setup>
import { computed } from 'vue'
import { GridContainerPreview } from '@common-ui/previews'
import { GeoJsonPreview } from '@common-ui-map'
import GRIBTemperatureMap from './GRIBTemperatureMap.vue'
import dataExplorerAPI from '../../api/dataExplorer'
const props = defineProps({ data: { type: Object, required: true }, locator: { type: String, required: true } })
const variables = computed(() => (props.data.object?.attributes?.type_info?.container?.children || [])
  .filter(child => child?.native)
  .map(child => ({ ...child.native, name: child.name })))
const gribSourceVersion = computed(() => props.data.object?.attributes?.format_info?.grib?.source_version || '')
async function readSlice(selection, signal) {
  const response = await dataExplorerAPI.getGridSlice(props.locator, selection, signal)
  const preview = response?.preview_type && response?.data ? response.data : (response?.data || response)
  return preview.object.content.json
}
function footprintPreview(spatial) {
  return { mode: 'object', source_srid: 4326, object: { content: { kind: 'geojson', geojson: spatial.footprint } } }
}
</script>
