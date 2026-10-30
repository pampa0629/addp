<template>
  <section data-testid="resource-block-capacity">
    <el-alert v-if="errorKey" :title="t(errorKey)" type="error" :closable="false" data-testid="resource-block-error" />
    <el-alert v-else-if="status !== 'available' && (!loading || observation)" :title="t(`monitor.resources.block.${status}`)" type="info" :closable="false" data-testid="resource-block-status" />
    <el-table :data="rows" data-testid="resource-block-table" :empty-text="t('monitor.resources.block.empty')">
      <el-table-column prop="dimensions.device" :label="t('monitor.resources.block.device')" min-width="140" />
      <el-table-column :label="t('monitor.resources.block.kind')" min-width="150"><template #default="{ row }">{{ t(`monitor.resources.block.kinds.${row.dimensions.kind}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resources.block.capacity')" min-width="140"><template #header><div class="capacity-heading"><span>{{ t('monitor.resources.block.capacity') }}</span><FieldHelp :content="t('monitor.resources.block.hint') + ' ' + t('monitor.resources.block.scope')" /></div></template><template #default="{ row }">{{ formatValue(row.metrics[metric.key], metric) }}</template></el-table-column>
      <el-table-column :label="t('monitor.resources.dataState')" min-width="130"><template #default="{ row }">{{ t(`monitor.resources.states.${row.metrics[metric.key].data_state}`) }}</template></el-table-column>
    </el-table>
  </section>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import { blockCapacityMetrics, resourceDimensionRows } from '../utils/nodeResources'
const props = defineProps({ observation: Object, errorKey: String, loading: Boolean, formatValue: { type: Function, required: true } })
const { t } = useI18n()
const metric = blockCapacityMetrics[0]
const status = computed(() => props.observation?.collection?.state === 'collecting' ? props.observation.collection.block_capacity || 'unknown' : 'unknown')
const rows = computed(() => status.value === 'available' && !props.errorKey ? resourceDimensionRows(props.observation) : [])
</script>
<style scoped>
.capacity-heading { display: flex; align-items: center; gap: 4px; }
</style>
