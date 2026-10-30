<template>
  <section class="device-resources" :data-testid="`resource-${family.name}s`">
    <h3 class="resource-metric-heading" :aria-label="t(`monitor.resources.${family.name}.title`)"><span>{{ t(`monitor.resources.${family.name}.title`) }}</span><FieldHelp :content="t(family.name === 'disk' ? 'monitor.resources.disk.orderHint' : 'monitor.resources.network.hint')" /></h3>
    <div v-if="family.name === 'disk' && family.rows.length" class="resource-toolbar" data-testid="resource-disk-display">
      <span class="resource-hint" role="status">{{ t('monitor.resources.disk.deviceCount', { shown: family.visibleRows.length, total: family.rows.length }) }}</span>
      <el-button v-if="family.collapsedCount" :aria-expanded="showAllDisks" data-testid="resource-disk-toggle" @click="$emit('update:showAllDisks', !showAllDisks)">{{ t(showAllDisks ? 'monitor.resources.disk.collapseIdle' : 'monitor.resources.disk.showAll', { total: family.rows.length }) }}</el-button>
    </div>
    <p v-if="family.name === 'disk' && family.rows.length && !family.visibleRows.length" class="resource-hint" role="status" data-testid="resource-disk-idle">{{ t('monitor.resources.disk.allIdle') }}</p>
    <el-alert v-if="family.errorKey" :title="t(family.errorKey)" type="error" :closable="false" :data-testid="`resource-${family.name}-error`" />
    <div v-if="(!loading || observation) && !family.errorKey && family.missingMessage" class="resource-empty-status" :data-testid="`resource-${family.name}-status`">
      <p class="resource-hint" role="status" :data-testid="`resource-${family.name}-empty`">{{ t(family.missingMessage) }}</p>
      <el-button v-if="!family.rows.length && canConfigure" text type="primary" @click="$emit('configure')">{{ t(configureLabel) }}</el-button>
    </div>
    <el-table v-if="!family.errorKey && family.visibleRows.length" :data="family.visibleRows" row-key="key" :data-testid="`resource-${family.name}-table`">
      <el-table-column prop="dimensions.device" :label="t(family.name === 'network' ? 'monitor.resources.network.interface' : 'monitor.resources.filesystem.device')" min-width="160" show-overflow-tooltip />
      <el-table-column v-for="metric in family.metrics" :key="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" min-width="170">
        <template #header>
          <div class="resource-metric-heading">
            <span>{{ t(`monitor.resources.metrics.${metric.name}`) }}</span>
            <FieldHelp v-if="family.name === 'disk'" :content="t(`monitor.resources.disk.columns.${metric.name}.detail`)" />
          </div>
        </template>
        <template #default="{ row }"><strong>{{ formatValue(row.metrics[metric.key], metric) }}</strong><p v-if="row.metrics[metric.key]" class="resource-hint">{{ t(`monitor.resources.states.${row.metrics[metric.key].data_state}`) }}</p></template>
      </el-table-column>
      <el-table-column :label="t('monitor.resources.trend')" width="140"><template #default="{ row }"><el-button text type="primary" @click="$emit('select', row, family.metrics[0].key)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button></template></el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import { resourceDetailGroups, resourceDimensionRows, sortDiskResourceRows, visibleDiskResourceRows, currentResourceValue, resourceMissingMessage } from '../utils/nodeResources'

const props = defineProps({
  familyName: { type: String, required: true, validator: value => value !== 'filesystem' && Object.hasOwn(resourceDetailGroups, value) },
  observation: { type: Object, default: null }, errorKey: { type: String, default: '' }, loading: Boolean,
  selectedDevice: { type: String, default: '' }, showAllDisks: Boolean, canConfigure: Boolean,
  configureLabel: { type: String, default: 'monitor.resources.configure' }, formatValue: { type: Function, required: true }
})
defineEmits(['update:showAllDisks', 'configure', 'select'])
const { t } = useI18n()
const family = computed(() => {
  const sourceRows = resourceDimensionRows(props.observation)
  const rows = props.familyName === 'disk' ? sortDiskResourceRows(sourceRows) : sourceRows
  const compactRows = props.familyName === 'disk' ? visibleDiskResourceRows(rows, props.selectedDevice) : rows
  return {
    name: props.familyName, metrics: resourceDetailGroups[props.familyName][0], rows,
    visibleRows: props.familyName === 'disk' && props.showAllDisks ? rows : compactRows,
    collapsedCount: rows.length - compactRows.length, errorKey: props.errorKey,
    missingMessage: (['not_collected', 'failed'].includes(props.observation?.collection[props.familyName]) || !rows.some(row => Object.values(row.metrics).some(point => currentResourceValue(point) !== null))) ? resourceMissingMessage(props.observation?.collection, props.familyName) : ''
  }
})
</script>

<style scoped>
.device-resources { margin-top: 24px; min-width: 0; }
.resource-hint { color: var(--addp-text-secondary); margin-top: 8px; font-size: 13px; }
.resource-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.resource-empty-status { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.resource-empty-status > .resource-hint { margin: 0; }
.resource-metric-heading { display: flex; align-items: center; gap: 4px; }
:deep(.el-table__header .cell) { white-space: normal; text-overflow: clip; }
</style>
