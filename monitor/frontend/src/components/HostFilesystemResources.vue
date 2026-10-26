<template>
  <section class="resource-filesystems" data-testid="resource-filesystems">
    <h3>{{ t('monitor.resources.filesystem.title') }}</h3>
    <p v-if="mountRows.length" class="resource-hint">{{ t('monitor.resources.filesystem.hint') }}</p>
    <p v-if="mountRows.length && capacity" class="resource-hint">{{ t('monitor.resources.queriedAt') }}: {{ date(capacity.queried_at) }}</p>
    <el-alert v-if="capacityErrorKey" :title="t(capacityErrorKey)" type="error" show-icon :closable="false" data-testid="resource-filesystem-error" />
    <el-alert v-if="inodeErrorKey" :title="`${t('monitor.resources.filesystem.inode')}: ${t(inodeErrorKey)}`" type="error" show-icon :closable="false" data-testid="resource-inode-error" />
    <p v-if="mountRows.length && inodes" class="resource-hint">{{ t('monitor.resources.filesystem.inode') }} · {{ t('monitor.resources.queriedAt') }}: {{ date(inodes.queried_at) }}</p>
    <div class="resource-empty-status" data-testid="resource-filesystem-status">
      <el-alert v-if="!loading && filesystemMissingMessage && ['not_collected', 'failed'].includes(capacity?.collection.filesystem)" :title="t(filesystemMissingMessage)" type="info" :closable="false" data-testid="resource-filesystem-capability" />
      <p v-else-if="!loading && filesystemMissingMessage" class="resource-hint" role="status" data-testid="resource-filesystem-empty">{{ t(filesystemMissingMessage) }}</p>
      <el-button v-if="!loading && !mountRows.length && !capacityErrorKey && !inodeErrorKey && canConfigure" text type="primary" @click="$emit('configure')">{{ t(configureLabel) }}</el-button>
    </div>
    <el-table v-if="mountRows.length" :data="mountRows" row-key="key" data-testid="resource-filesystem-table">
      <el-table-column prop="dimensions.device" :label="t('monitor.resources.filesystem.device')" min-width="160" show-overflow-tooltip />
      <el-table-column prop="dimensions.mountpoint" :label="t('monitor.resources.filesystem.mountpoint')" min-width="180" show-overflow-tooltip />
      <el-table-column prop="dimensions.fstype" :label="t('monitor.resources.filesystem.type')" min-width="100" show-overflow-tooltip />
      <el-table-column v-for="metric in mountMetrics" :key="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" min-width="170">
        <template #default="{ row }"><strong>{{ formatValue(row.metrics[metric.key], metric) }}</strong><p v-if="row.metrics[metric.key]" class="resource-hint">{{ t(`monitor.resources.states.${row.metrics[metric.key].data_state}`) }}</p><p v-if="row.metrics[metric.key]" class="resource-hint">{{ date(row.metrics[metric.key].sampled_at) }}</p></template>
      </el-table-column>
      <el-table-column :label="t('monitor.resources.trend')" width="140" fixed="right"><template #default="{ row }"><el-button text type="primary" @click="$emit('select', row, filesystemMetrics[0].key)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button></template></el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { filesystemMetrics, mountMetrics, filesystemResourceState } from '../utils/nodeResources'
const props = defineProps({
  capacity: { type: Object, default: null }, inodes: { type: Object, default: null },
  capacityErrorKey: { type: String, default: '' }, inodeErrorKey: { type: String, default: '' }, loading: Boolean,
  canConfigure: Boolean, configureLabel: { type: String, default: 'monitor.resources.configure' }, formatValue: { type: Function, required: true }
})
defineEmits(['configure', 'select'])
const { t, locale } = useI18n()
const state = computed(() => filesystemResourceState(props.capacity, props.inodes, props.capacityErrorKey, props.inodeErrorKey))
const mountRows = computed(() => state.value.rows)
const filesystemMissingMessage = computed(() => state.value.missingMessage)
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
</script>

<style scoped>
.resource-filesystems { margin-top: 24px; min-width: 0; }
.resource-hint { color: var(--addp-text-secondary); margin-top: 8px; font-size: 13px; }
.resource-empty-status { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.resource-empty-status > .el-alert { flex: 1; min-width: 0; margin: 0; }
.resource-empty-status > .resource-hint { margin: 0; }
:deep(.el-table__header .cell) { white-space: normal; text-overflow: clip; }
</style>
