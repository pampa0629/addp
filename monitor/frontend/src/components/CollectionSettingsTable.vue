<template>
  <el-table :data="rows" border stripe>
    <el-table-column :label="t('monitor.targets.object')" min-width="155">
      <template #default="{ row }"><slot name="object" :row="row" /></template>
    </el-table-column>
    <el-table-column :label="t('monitor.targets.kind')" min-width="110">
      <template #default="{ row }"><slot name="kind" :row="row" /></template>
    </el-table-column>
    <el-table-column :label="t('monitor.targets.configState')" min-width="100">
      <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'monitor.targets.enabled' : 'monitor.targets.disabled') }}</el-tag></template>
    </el-table-column>
    <el-table-column v-if="$slots.observation" :label="t('monitor.targets.automatic.observation')" min-width="165">
      <template #default="{ row }"><slot name="observation" :row="row" /></template>
    </el-table-column>
    <el-table-column v-if="canToggle || $slots.actions" :label="t('monitor.targets.automatic.operation')" min-width="110">
      <template #default="{ row }">
        <el-popconfirm v-if="canToggle" :title="t(row.enabled ? 'monitor.targets.automatic.confirmPause' : 'monitor.targets.confirmResume')"
          :confirm-button-text="t('common.confirm')" :cancel-button-text="t('common.cancel')" @confirm="$emit('toggle', row)">
          <template #reference>
            <el-button text type="primary" :disabled="busy || toggleDisabled?.(row)">{{ t(row.enabled ? 'monitor.targets.automatic.pause' : 'monitor.targets.automatic.resume') }}</el-button>
          </template>
        </el-popconfirm>
        <slot name="actions" :row="row" />
      </template>
    </el-table-column>
    <template #empty><el-empty :description="empty" :image-size="64" /></template>
  </el-table>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
defineProps({ rows: { type: Array, required: true }, empty: { type: String, required: true }, canToggle: Boolean, toggleDisabled: Function, busy: Boolean })
defineEmits(['toggle'])
const { t } = useI18n()
</script>
