<template>
  <section class="agent-map-view">
    <MapContainer
      :features="properties.features"
      :height="`${properties.height || 360}px`"
      features-only
    />
    <p v-if="properties.truncated" class="projection-note">
      {{ t('agent.chat.presentation.mapTruncated') }}
    </p>
  </section>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MapContainer } from '@addp/common-frontend/map'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
.agent-map-view {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}

.projection-note {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}
</style>
