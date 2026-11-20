<template>
  <section class="frontend-detail">
    <h4>{{ t('monitor.frontend.title') }} · {{ nodeName }}</h4>
    <dl class="instance-metrics">
      <div v-for="[label, value] in fields" :key="label">
        <dt>{{ t(`monitor.frontend.${label}`) }}</dt>
        <dd>{{ value }}</dd>
      </div>
    </dl>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatBytes } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'

const props = defineProps({ row: { type: Object, required: true }, nodeName: String })
const { t, locale } = useI18n()
const reachability = value => t(`monitor.frontend.states.${value === true ? 'up' : value === false ? 'down' : 'unknown'}`)
const fields = computed(() => {
  const row = props.row
  return [
    ['runtime', row.mode ? t(`monitor.frontend.modes.${row.mode}`) : '—'],
    ['restarts', row.restart_count ?? '—'],
    ['entry', reachability(row.entry_up)], ['asset', reachability(row.asset_up)],
    ['cpu', row.cpu_cores == null ? '—' : formatFieldPresentationValue(row.cpu_cores, { precision: 2, unit: t('monitor.processTotals.cores') }, locale.value)],
    ['memory', row.memory_bytes == null ? '—' : formatBytes(row.memory_bytes, 2, locale.value)]
  ]
})
</script>
