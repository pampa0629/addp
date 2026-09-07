<template>
  <div class="scalar-value-renderer">
    <article v-for="item in displayItems" :key="item.field" class="value-card" :class="item.state ? `state--${item.state.tone}` : ''" :aria-label="item.label">
      <div class="value-heading">
        <div class="value-label">{{ item.label }}</div>
        <span v-if="item.state" class="state-indicator">{{ item.state.label }}</span>
      </div>
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
import { presentFieldValue } from '../utils/fieldPresentation.mjs'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  config: { type: Object, required: true },
  fields: { type: Array, default: () => [] },
})

const { locale } = useI18n()
const fieldFacts = computed(() => Object.fromEntries(props.fields.map((field) => [field.name, field])))
const displayItems = computed(() => (props.config.items || []).map((item) => {
  const rawValue = props.rows[0]?.[item.field]
  return {
    ...item,
    label: item.label || fieldFacts.value[item.field]?.comment || item.field,
    value: formatScalarValue(rawValue, item.precision, locale.value),
    state: presentFieldValue(rawValue, { state_rules: item.state_rules || [] }, locale.value).state,
  }
}))
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

.value-card.state--info { border-color: var(--el-color-info); }
.value-card.state--success { border-color: var(--el-color-success); }
.value-card.state--warning { border-color: var(--el-color-warning); }
.value-card.state--danger { border-color: var(--el-color-danger); }

.value-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.state-indicator {
  flex: 0 0 auto;
  padding: 1px 6px;
  border: 1px solid currentColor;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  line-height: 18px;
}

.state--info .state-indicator { color: var(--el-color-info); background: var(--el-color-info-light-9); }
.state--success .state-indicator { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.state--warning .state-indicator { color: var(--el-color-warning); background: var(--el-color-warning-light-9); }
.state--danger .state-indicator { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }

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
