<template>
  <div class="scalar-value-renderer">
    <article v-for="item in displayItems" :key="item.field" class="value-card" :aria-label="item.label">
      <div class="value-label">{{ item.label }}</div>
      <div class="value-content">
        <span class="value-number">{{ item.value }}</span>
        <span v-if="item.unit" class="value-unit">{{ item.unit }}</span>
      </div>
    </article>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScalarValue } from '../utils/scalarValueResult.mjs'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  config: { type: Object, required: true },
  fields: { type: Array, default: () => [] },
})

const { locale } = useI18n()
const fieldFacts = computed(() => Object.fromEntries(props.fields.map((field) => [field.name, field])))
const displayItems = computed(() => (props.config.items || []).map((item) => ({
  ...item,
  label: item.label || fieldFacts.value[item.field]?.comment || item.field,
  value: formatScalarValue(props.rows[0]?.[item.field], item.precision, locale.value),
})))
</script>

<style scoped>
.scalar-value-renderer {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
  width: 100%;
}

.value-card {
  min-width: 0;
  padding: 16px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
}

.value-label {
  overflow: hidden;
  color: var(--addp-text-secondary);
  font-size: 14px;
  line-height: 1.5;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.value-content {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-top: 8px;
}

.value-number {
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: clamp(24px, 3vw, 40px);
  font-weight: 600;
  line-height: 1.2;
  text-overflow: ellipsis;
}

.value-unit {
  flex: 0 0 auto;
  color: var(--addp-text-secondary);
  font-size: 14px;
}
</style>

