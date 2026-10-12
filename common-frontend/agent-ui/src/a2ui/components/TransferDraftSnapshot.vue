<template>
  <section class="draft-snapshot" data-testid="transfer-draft-snapshot">
    <h4>{{ t('agent.chat.presentation.draft.title') }} · {{ properties.name }}</h4>
    <p>{{ t('agent.chat.presentation.draft.notice') }}</p>
    <p class="draft-reference">{{ t('agent.chat.presentation.draft.reference') }}: {{ properties.reference }}</p>
    <details>
      <summary>{{ t('agent.chat.presentation.draft.mappings') }} ({{ properties.mappings.length }})</summary>
      <div class="draft-table">
        <TabularResultRenderer :rows="properties.mappings" :columns="columns" :max-height="420" size="small"
          :null-text="t('agent.chat.presentation.draft.unspecified')" />
      </div>
    </details>
    <details>
      <summary>{{ t('agent.chat.presentation.draft.parameters') }}</summary>
      <JsonPreview :data="{ object: { content: { text: properties.parameters } } }" />
    </details>
  </section>
</template>

<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TabularResultRenderer from '../../../../basic/src/components/TabularResultRenderer.vue'
import JsonPreview from '../../../../basic/src/components/previews/JsonPreview.vue'

const props = defineProps({ context: { type: Object, required: true }, buildChild: { type: Function, default: null } })
const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => { properties.value = { ...component.properties } })
onUnmounted(() => subscription.unsubscribe())
const columns = computed(() => ['source', 'target', 'target_type', 'nullable', 'precision', 'scale'].map(key => ({
  key, label: t(`agent.chat.presentation.draft.${key}`), minWidth: 120
})))
</script>

<style scoped>
.draft-snapshot { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; color: var(--addp-text-primary); background: var(--addp-bg-primary); min-width: 0; }
h4 { margin: 0; }
p, summary { overflow-wrap: anywhere; }
.draft-reference { color: var(--addp-text-secondary); font-size: 12px; }
details { margin-top: 12px; }
summary { cursor: pointer; }
.draft-table { min-width: 0; overflow-x: auto; margin-top: 8px; }
</style>
