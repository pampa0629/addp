<template>
  <el-empty v-if="!process" :description="t('develop.workflowProcess.noSnapshot')" />
  <div v-else class="workflow-process">
    <section aria-labelledby="workflow-process-nodes">
      <h3 id="workflow-process-nodes">{{ t('develop.workflowProcess.nodes') }}</h3>
      <el-table :data="process.nodes" border row-key="id" data-testid="workflow-process-nodes">
        <el-table-column prop="id" :label="t('develop.workflowProcess.node')" min-width="120" />
        <el-table-column prop="operator" :label="t('develop.workflowProcess.operator')" min-width="130" />
        <el-table-column :label="t('develop.workflowProcess.observedStatus')" min-width="145">
          <template #default="{ row }">
            <el-tag :type="statusColors[row.status] || 'info'" effect="plain">
              {{ t('develop.workflowProcess.status.' + row.status) }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </section>
    <section aria-labelledby="workflow-process-links">
      <h3 id="workflow-process-links">{{ t('develop.workflowProcess.links') }}</h3>
      <p>{{ t('develop.workflowProcess.linkMeaning') }}</p>
      <el-table :data="process.definition_links" border :empty-text="t('develop.workflowProcess.noLinks')"
        data-testid="workflow-process-links">
        <el-table-column prop="from_node" :label="t('develop.workflowProcess.fromNode')" min-width="112" />
        <el-table-column :label="t('develop.workflowProcess.outputPort')" min-width="120">
          <template #default="{ row }">
            {{ row.output_port || t('develop.workflowProcess.defaultPort') }}
          </template>
        </el-table-column>
        <el-table-column prop="to_node" :label="t('develop.workflowProcess.toNode')" min-width="112" />
        <el-table-column prop="input_parameter" :label="t('develop.workflowProcess.inputParameter')" min-width="125" />
      </el-table>
    </section>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

defineProps({
  process: { type: Object, default: null }
})

const { t } = useI18n()
const statusColors = { observed: 'primary', succeeded: 'success', failed: 'danger' }
</script>

<style scoped>
.workflow-process {
  display: grid;
  gap: 24px;
}

.workflow-process section {
  min-width: 0;
}

.workflow-process h3 {
  margin: 0 0 12px;
  font-size: 16px;
}

.workflow-process p {
  margin: 0 0 12px;
  color: var(--addp-text-secondary);
}
</style>
