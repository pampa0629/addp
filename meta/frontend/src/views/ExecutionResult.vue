<template>
  <ExecutionResultPage
    module="meta"
    :execution-id="$route.params.execution_id"
    :return-to="$route.query.return_to"
    :read="metaApi.getScanRun" :title="t('meta.executionResult.title')"
    v-slot="{ execution }"
  >
    <el-descriptions border :column="2">
      <el-descriptions-item v-if="execution.scan_context?.engine_id" :label="t('meta.executionResult.engine')">{{ execution.scan_context.engine_id }}</el-descriptions-item>
      <el-descriptions-item v-if="execution.scan_context?.scan_depth" :label="t('meta.scan.scanDepth')">{{ t(execution.scan_context.scan_depth === 'deep' ? 'meta.scan.deepScan' : 'meta.scan.basicScan') }}</el-descriptions-item>
      <template v-if="execution.scan_progress">
        <el-descriptions-item v-if="execution.scan_progress.items_total_known" :label="t('meta.executionResult.planned')">{{ execution.scan_progress.items_planned }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.executionResult.completed')">{{ execution.scan_progress.items_completed }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.executionResult.succeeded')">{{ execution.scan_progress.items_succeeded }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.executionResult.failed')">{{ execution.scan_progress.items_failed }}</el-descriptions-item>
      </template>
    </el-descriptions>
    <p v-if="!execution.scan_progress">{{ t('common.executionMonitor.emptyResult') }}</p>
  </ExecutionResultPage>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ExecutionResultPage } from '@common-ui'
import metaApi from '../api/meta'
const { t } = useI18n()
</script>
