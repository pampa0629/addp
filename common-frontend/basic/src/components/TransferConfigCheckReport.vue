<template>
  <section class="transfer-config-check-report" data-testid="task-config-check-report" aria-live="polite">
    <header>
      <h4>{{ t('transferConfigCheck.title') }}</h4>
      <el-tag :type="statusType(report.status)">{{ t(`transferConfigCheck.status.${report.status}`) }}</el-tag>
      <span class="observed-at">{{ t('transferConfigCheck.observedAt') }} {{ report.observed_at }}</span>
    </header>
    <p class="report-notice">{{ t('transferConfigCheck.snapshotNotice') }}</p>
    <div class="check-scope-grid">
      <details v-for="[key, label] in sections" :key="key">
        <summary>{{ t(`transferConfigCheck.${label}`) }} ({{ report[key].length }})</summary>
        <ul><li v-for="(item, index) in report[key]" :key="index">{{ labelFor('checks', item) }}</li></ul>
      </details>
    </div>
    <details v-if="report.diagnostics.length" class="diagnostics">
      <summary>{{ t('transferConfigCheck.diagnostics') }} ({{ report.diagnostics.length }})</summary>
      <div class="diagnostics-scroll">
        <el-table :data="report.diagnostics" :max-height="420" size="small">
          <el-table-column :label="t('transferConfigCheck.conclusion')" min-width="210">
            <template #default="{ row }">
              <el-tag size="small" :type="statusType(row.status)">{{ t(`transferConfigCheck.status.${row.status}`) }}</el-tag>
              <div>{{ labelFor('codes', row.code) }}</div>
              <code>{{ row.code }}</code>
            </template>
          </el-table-column>
          <el-table-column :label="t('transferConfigCheck.location')" min-width="240">
            <template #default="{ row }"><code>{{ row.config_path }}</code></template>
          </el-table-column>
          <el-table-column prop="source_field" :label="t('transferConfigCheck.sourceField')" min-width="120" />
          <el-table-column prop="target_field" :label="t('transferConfigCheck.targetField')" min-width="120" />
          <el-table-column prop="physical_type" :label="t('transferConfigCheck.physicalType')" min-width="120" />
        </el-table>
      </div>
    </details>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

defineProps({ report: { type: Object, required: true } })
const { t, te } = useI18n()
const sections = [['checked', 'checked'], ['not_checked', 'notChecked'], ['runtime_checks_required', 'runtimeRequired']]
const statusType = status => ({ supported: 'success', unsupported: 'danger', unknown: 'warning' })[status]
function labelFor(group, code) {
  const key = `transferConfigCheck.${group}.${code}`
  return te(key) ? t(key) : code
}
</script>

<style scoped>
.transfer-config-check-report {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  overflow: hidden;
}
header { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
h4 { margin: 0; }
.observed-at, .report-notice { color: var(--addp-text-secondary); font-size: 12px; overflow-wrap: anywhere; }
.check-scope-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr)); gap: 16px; }
summary { cursor: pointer; overflow-wrap: anywhere; }
summary:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
ul { padding-left: 20px; }
li, code { overflow-wrap: anywhere; }
.diagnostics { margin-top: 16px; }
.diagnostics-scroll { width: 100%; overflow-x: auto; margin-top: 8px; }
</style>
