<template>
  <el-card class="source-summary" shadow="never">
    <template #header>
      <div class="source-header">
        <strong>{{ t('model.metric_workspace.source_summary') }}</strong>
        <span v-if="revision">{{ t('model.metric_workspace.saved_sources', { revision: revision.revision_no }) }}</span>
      </div>
    </template>
    <el-alert
      v-if="pending"
      :title="t('model.metric_workspace.sources_pending')"
      type="info" :closable="false"
    />
    <el-descriptions v-if="showImplementation || sourceRows.length" :column="1">
      <el-descriptions-item v-if="showImplementation" :label="t('model.metric_workspace.title')">
        {{ implementation.name }}
      </el-descriptions-item>
      <el-descriptions-item v-if="sourceRows.length" :label="t('model.metric_workspace.execution_engine')">
        {{ sourceEngineLabel }}
      </el-descriptions-item>
    </el-descriptions>
    <template v-if="sourceRows.length">
      <p v-if="engineError" class="source-notice">{{ t('model.metric_workspace.engine_name_unavailable') }}</p>
      <el-table :data="sourceRows" row-key="id">
        <el-table-column :label="t(canNavigate ? 'model.metric_workspace.logical_source' : 'model.logical_table.title')" min-width="150">
          <template #default="{ row }">
            <el-button
              v-if="row.table && canNavigate"
              link type="primary"
              @click="emit('open-table', row.id)"
            >{{ row.table.name }}</el-button>
            <span v-else>{{ row.table?.name || t('model.metric_workspace.logical_table_id', { id: row.id }) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.metric_workspace.source_role')" width="100">
          <template #default="{ row }">{{ t(`model.metric_workspace.${row.id === implementation.fact_table_id ? 'fact_source' : 'dimension_source'}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('model.metric_workspace.physical_table')" min-width="220">
          <template #default="{ row }"><span class="source-location">{{ row.physicalTable }}</span></template>
        </el-table-column>
      </el-table>
    </template>
    <p v-else class="source-notice">{{ t(`model.metric_workspace.${revision ? 'sources_unavailable' : 'sources_unsaved'}`) }}</p>
  </el-card>
</template>
<script setup>
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { formatLocatorDisplayPath } from '@common-ui';

const props = defineProps({
  implementation: { type: Object, required: true },
  revision: { type: Object, default: null },
  tables: { type: Array, default: () => [] },
  engines: { type: Array, default: () => [] },
  engineError: Boolean,
  pending: Boolean,
  canNavigate: Boolean,
  showImplementation: Boolean,
});
const emit = defineEmits(['open-table']);
const { t } = useI18n();

// Binding facts always belong to the selected saved revision, including drafts.
const sourceSnapshot = computed(() => props.revision?.dependency_snapshot);
const sourceRows = computed(() => {
  if (!sourceSnapshot.value?.execution_plan?.engine_id) return [];
  return Object.values(sourceSnapshot.value.tables || {})
    .map(source => ({
      id: source.id,
      table: props.tables.find(table => table.id === source.id),
      physicalTable: formatLocatorDisplayPath(source.locator, {
        appendedPath: [source.name], resourceType: 'table',
      }),
    }))
    .sort((a, b) => Number(b.id === props.implementation.fact_table_id) - Number(a.id === props.implementation.fact_table_id) || a.id - b.id);
});
const sourceEngineLabel = computed(() => {
  const id = sourceSnapshot.value?.execution_plan?.engine_id;
  const engine = props.engines.find(engine => Number(engine.id) === id);
  return engine
    ? t('model.metric_workspace.named_engine', { name: engine.name, id })
    : t('model.metric_workspace.engine_id', { id });
});
</script>
<style scoped>
.source-summary {
  min-width: 0;
}
.source-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.source-location {
  overflow-wrap: anywhere;
}
.source-notice {
  color: var(--addp-text-secondary);
}
</style>
