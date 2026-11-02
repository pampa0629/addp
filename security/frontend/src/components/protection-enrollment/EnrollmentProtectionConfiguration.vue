<template>
  <section class="protection-configuration" :aria-label="t('security.policy.configuration.title')">
    <dl class="protection-configuration__methods">
      <div class="protection-configuration__baseline">
        <dt>{{ t('security.policy.configuration.baseline') }}</dt>
        <dd><strong class="protection-method">{{ presentation.baseline?.method || t('security.policy.baselineMissing') }}</strong></dd>
      </div>
      <div v-for="field in presentation.fields" :key="field.key" class="protection-configuration__field">
        <dt>{{ t('security.policy.configuration.fieldScope', { scope: field.scope }) }}</dt>
        <dd><strong class="protection-method">{{ field.method }}</strong></dd>
      </div>
    </dl>
    <p v-if="!presentation.canReadPolicies" class="protection-configuration__hint">{{ t('security.policy.configuration.unreadable') }}</p>
    <p class="protection-configuration__hint">{{ t('security.policy.configuration.baselineHint') }}</p>
    <details v-if="presentation.baseline || presentation.fields.length" class="protection-configuration__details">
      <summary>{{ t('security.policy.configuration.parameters') }}</summary>
      <dl>
        <div v-if="presentation.baseline">
          <dt>{{ t('security.policy.configuration.baseline') }}</dt>
          <dd>{{ presentation.baseline.details }}</dd>
        </div>
        <div v-for="field in presentation.fields" :key="field.key">
          <dt>{{ t('security.policy.configuration.fieldScope', { scope: field.scope }) }}</dt>
          <dd>{{ field.details }}</dd>
        </div>
      </dl>
    </details>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
defineProps({ presentation: { type: Object, required: true } })
const { t } = useI18n()
</script>

<style scoped>
.protection-configuration { min-width: 0; margin-top: 12px; padding: 12px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.protection-configuration dl { display: flex; flex-direction: column; gap: 8px; margin: 0; }
.protection-configuration dl > div { display: grid; grid-template-columns: minmax(120px, .7fr) minmax(0, 1fr); gap: 8px; font-size: 12px; line-height: 1.5; }
.protection-configuration dt { color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.protection-configuration dd { min-width: 0; margin: 0; color: var(--addp-text-primary); overflow-wrap: anywhere; }
.protection-method { font-weight: 600; }
.protection-configuration__hint { margin: 8px 0 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.protection-configuration__details { margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--addp-border-color); }
.protection-configuration__details summary { cursor: pointer; color: var(--el-color-primary); font-size: 12px; }
.protection-configuration__details dl { margin-top: 10px; }
@container finding-card (max-width: 420px) {
  .protection-configuration dl > div { grid-template-columns: 1fr; gap: 2px; }
}
</style>
