<template>
  <section class="agent-table-preview">
    <div class="table-scroll">
      <el-table
        :data="properties.rows"
        :style="{ minWidth: tableMinWidth }"
        size="small"
        max-height="420"
      >
        <el-table-column
          v-for="column in properties.columns"
          :key="column"
          :label="column"
          :prop="column"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">{{ formatCell(row[column]) }}</template>
        </el-table-column>
      </el-table>
    </div>
    <p class="table-summary">
      {{ t('agent.chat.presentation.tableSummary', { visible: properties.rows.length, total: properties.total }) }}
      <span v-if="properties.truncated"> · {{ t('agent.chat.presentation.truncated') }}</span>
    </p>
  </section>
</template>

<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})

const formatCell = value => value === null ? t('agent.chat.presentation.nullValue') : String(value)
const tableMinWidth = computed(() => `${Math.max(360, properties.value.columns.length * 120)}px`)

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
.agent-table-preview {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  overflow: hidden;
}

.table-scroll {
  box-sizing: border-box;
  width: 100%;
  overflow-x: auto;
}

.table-summary {
  margin: 0;
  padding: 8px 12px;
  border-top: 1px solid var(--addp-border-color-light);
  font-size: 12px;
  color: var(--addp-text-tertiary);
}
</style>
