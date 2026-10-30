<template>
  <section class="draft-snapshot" data-testid="transfer-draft-snapshot">
    <h4>{{ t('agent.chat.presentation.draft.title') }} · {{ properties.name }}</h4>
    <p>{{ t('agent.chat.presentation.draft.notice') }}</p>
    <dl class="draft-overview" :aria-label="t('agent.chat.presentation.draft.overview')">
      <template v-for="row in overview" :key="row.key">
        <dt>{{ t(`agent.chat.presentation.draft.${row.key}`) }}</dt>
        <dd :data-testid="`draft-${row.key}`">{{ row.value }}</dd>
      </template>
    </dl>
    <details>
      <summary>{{ t('agent.chat.presentation.draft.mappings') }} ({{ properties.mappings.length }})</summary>
      <div class="draft-table">
        <TabularResultRenderer :rows="properties.mappings" :columns="columns" :max-height="420" size="small"
          :null-text="t('agent.chat.presentation.draft.unspecified')" />
      </div>
    </details>
    <details>
      <summary>{{ t('agent.chat.presentation.draft.parameters') }}</summary>
      <p class="draft-reference">{{ t('agent.chat.presentation.draft.reference') }}: {{ properties.reference }}</p>
      <JsonPreview :data="{ object: { content: { text: properties.parameters } } }" />
    </details>
  </section>
</template>

<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TabularResultRenderer from '../../../../basic/src/components/TabularResultRenderer.vue'
import JsonPreview from '../../../../basic/src/components/previews/JsonPreview.vue'
import { formatLocatorDisplayPath, parseLocatorSafe } from '../../../../basic/src/types/resourceLocator.js'

const props = defineProps({ context: { type: Object, required: true }, buildChild: { type: Function, default: null } })
const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => { properties.value = { ...component.properties } })
onUnmounted(() => subscription.unsubscribe())
const parameters = computed(() => JSON.parse(properties.value.parameters))
const literal = value => typeof value === 'string' && value ? value : t('agent.chat.presentation.draft.unspecified')
function location(uri, options = {}) {
  const parsed = parseLocatorSafe(uri)
  if (!parsed.engineId) return literal(uri)
  return t('agent.chat.presentation.draft.location', { engine: parsed.engineId,
    path: formatLocatorDisplayPath(uri, options) || t('agent.chat.presentation.draft.unspecified') })
}
const overview = computed(() => {
  const config = parameters.value.config
  const target = config?.target
  const policy = target?.policy?.apply_mode
  return [
    { key: 'sourceLocation', value: location(config?.source?.locator) },
    { key: 'targetLocation', value: location(target?.parent_locator, {
      resourceType: target?.data_type, appendedPath: typeof target?.name === 'string' ? [target.name] : []
    }) },
    { key: 'boundary', value: literal(config?.runtime?.boundary) },
    { key: 'loadMode', value: literal(config?.load?.mode) },
    { key: 'applyPolicy', value: policy === 'replace' || policy === 'append'
      ? t(`agent.chat.presentation.draft.${policy}`) : literal(policy) },
    { key: 'mappingCount', value: properties.value.mappings.length }
  ]
})
const columns = computed(() => ['source', 'target', 'target_type', 'nullable', 'precision', 'scale'].map(key => ({
  key, label: t(`agent.chat.presentation.draft.${key}`), minWidth: 120
})))
</script>

<style scoped>
.draft-snapshot { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; color: var(--addp-text-primary); background: var(--addp-bg-primary); min-width: 0; }
h4 { margin: 0; overflow-wrap: anywhere; }
p, summary { overflow-wrap: anywhere; }
.draft-reference { color: var(--addp-text-secondary); font-size: 12px; }
.draft-overview { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; margin: 12px 0; }
.draft-overview dt { color: var(--addp-text-secondary); }
.draft-overview dd { margin: 0; overflow-wrap: anywhere; }
details { margin-top: 12px; }
summary { cursor: pointer; }
.draft-table { min-width: 0; overflow-x: auto; margin-top: 8px; }
</style>
