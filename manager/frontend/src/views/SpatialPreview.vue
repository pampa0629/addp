<template>
  <div class="page">
    <div class="toolbar">
      <div class="field">{{ schema }}.{{ table }}</div>
      <div class="spacer" />
    </div>
    <div class="map-wrap">
      <VectorTilePreview
        v-if="ready"
        :resource-id="engineId"
        :schema="schema"
        :table="table"
        :geom="geom"
        :cols="cols"
        :srid="srid"
      />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'

const route = useRoute()

const engineId = computed(() => Number(route.query.engine_id || route.query.engineId || 0))
const schema = computed(() => String(route.query.schema || 'public'))
const table = computed(() => String(route.query.table || ''))
const geom = computed(() => String(route.query.geom || 'geom'))
const srid = computed(() => Number(route.query.srid || 4326))
const cols = computed(() => String(route.query.cols || '').split(',').filter(Boolean))
const ready = computed(() => !!(engineId.value && schema.value && table.value))
</script>

<style scoped>
.page { position: relative; width: 100%; height: 100%; display: flex; flex-direction: column; }
.toolbar { height: 44px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid #eee; }
.spacer { flex: 1; }
.map-wrap { position: relative; flex: 1; }
</style>
