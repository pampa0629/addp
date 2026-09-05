<template>
  <section v-if="hasLineage" class="execution-lineage" aria-labelledby="execution-lineage-title">
    <div class="execution-lineage__heading">
      <div>
        <h4 id="execution-lineage-title">{{ t('monitor.execution.detail.lineage.title') }}</h4>
        <p>{{ t('monitor.execution.detail.lineage.description') }}</p>
      </div>
      <el-tag effect="plain" size="small">
        {{ t('monitor.execution.detail.lineage.resource_count', { count: resourceCount }) }}
      </el-tag>
    </div>

    <div class="execution-lineage__columns">
      <div class="execution-lineage__group">
        <h5>
          {{ t('monitor.execution.detail.lineage.inputs') }}
          <span>{{ summary.inputs.length }}</span>
        </h5>
        <div v-if="summary.inputs.length" class="execution-lineage__list">
          <ResourceFactCard
            v-for="resource in summary.inputs"
            :key="resource.key"
            :resource="resource"
            @open="openResource"
          />
        </div>
        <div v-else class="execution-lineage__empty">
          {{ t('monitor.execution.detail.lineage.no_inputs') }}
        </div>
      </div>

      <div class="execution-lineage__group">
        <h5>
          {{ t('monitor.execution.detail.lineage.outputs') }}
          <span>{{ summary.outputs.length }}</span>
        </h5>
        <div v-if="summary.outputs.length" class="execution-lineage__list">
          <ResourceFactCard
            v-for="resource in summary.outputs"
            :key="resource.key"
            :resource="resource"
            @open="openResource"
          />
        </div>
        <div v-else class="execution-lineage__empty">
          {{ t('monitor.execution.detail.lineage.no_outputs') }}
        </div>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed, defineComponent, h } from 'vue'
import { ElButton, ElMessage, ElTag, ElTooltip } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  buildExecutionLineageSummary,
  buildManagerDataExplorerRoute,
  openConsoleRoute
} from '@common-ui'

const props = defineProps({
  metadata: { type: Object, default: () => ({}) }
})

const { t, te } = useI18n()
const summary = computed(() => buildExecutionLineageSummary(props.metadata))
const resourceCount = computed(() => summary.value.inputs.length + summary.value.outputs.length)
const hasLineage = computed(() => Boolean(summary.value.schemaVersion) && resourceCount.value > 0)

function translatedValue(group, value, fallback) {
  if (!value) return fallback
  const key = `monitor.execution.detail.lineage.${group}.${value}`
  return te(key) ? t(key) : value
}

async function openResource(resource) {
  if (resource?.direction !== 'input' || !resource.explorable) return
  try {
    await openConsoleRoute(buildManagerDataExplorerRoute(resource.locator))
  } catch {
    ElMessage.error(t('monitor.execution.detail.lineage.open_failed'))
  }
}

const ResourceFactCard = defineComponent({
  name: 'ExecutionLineageResourceFact',
  emits: ['open'],
  props: {
    resource: { type: Object, required: true }
  },
  setup(cardProps, { emit }) {
    return () => {
      const resource = cardProps.resource
      const details = []

      if (resource.resourceType) {
        details.push(h(ElTag, { size: 'small', effect: 'plain' }, () => (
          translatedValue('resource_types', resource.resourceType, resource.resourceType)
        )))
      }
      if (resource.platformInternal) {
        details.push(h(ElTag, { size: 'small', type: 'info', effect: 'plain' }, () => (
          t('monitor.execution.detail.lineage.platform_internal_artifact')
        )))
      }
      if (resource.itemId) {
        details.push(h('span', { class: 'execution-lineage__item-id' }, (
          t('monitor.execution.detail.lineage.item_id', { id: resource.itemId })
        )))
      }
      if (resource.writeMode) {
        details.push(h('span', { class: 'execution-lineage__write-mode' }, (
          t('monitor.execution.detail.lineage.write_mode', {
            mode: translatedValue('write_modes', resource.writeMode, resource.writeMode)
          })
        )))
      }

      return h('article', { class: 'execution-lineage__card' }, [
        h('div', { class: 'execution-lineage__resource-main' }, [
          resource.port
            ? h(ElTag, { size: 'small', type: 'primary', effect: 'dark' }, () => resource.port)
            : null,
          h(ElTooltip, { content: resource.locator, placement: 'top', showAfter: 400 }, () => (
            h('strong', { class: 'execution-lineage__resource-name' }, resource.displayName)
          )),
          resource.direction === 'input' && resource.explorable
            ? h(ElButton, {
              link: true,
              type: 'primary',
              class: 'execution-lineage__resource-action',
              onClick: () => emit('open', resource)
            }, () => t('monitor.execution.detail.lineage.open_in_data_explorer'))
            : null
        ]),
        details.length ? h('div', { class: 'execution-lineage__resource-meta' }, details) : null
      ])
    }
  }
})
</script>

<style scoped>
.execution-lineage {
  margin-top: 20px;
}

.execution-lineage__heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.execution-lineage__heading h4,
.execution-lineage__heading p,
.execution-lineage__group h5 {
  margin: 0;
}

.execution-lineage__heading p {
  margin-top: 4px;
  color: var(--addp-text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.execution-lineage__columns {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.execution-lineage__group {
  min-width: 0;
  padding: 14px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
}

.execution-lineage__group h5 {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.execution-lineage__group h5 span {
  color: var(--addp-text-tertiary);
  font-size: 12px;
  font-weight: 400;
}

.execution-lineage__list {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}

.execution-lineage__card {
  min-width: 0;
  padding: 10px 12px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color-light);
  border-radius: 4px;
}

.execution-lineage__resource-main,
.execution-lineage__resource-meta {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 8px;
}

.execution-lineage__resource-name {
  min-width: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.execution-lineage__resource-action {
  flex-shrink: 0;
  margin-left: auto;
}

.execution-lineage__resource-meta {
  flex-wrap: wrap;
  margin-top: 8px;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.execution-lineage__empty {
  padding: 22px 8px 8px;
  color: var(--addp-text-tertiary);
  font-size: 13px;
  text-align: center;
}

@media (max-width: 900px) {
  .execution-lineage__columns {
    grid-template-columns: 1fr;
  }
}
</style>
