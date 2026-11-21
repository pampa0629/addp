<template>
  <ExecutionResultPage
    module="graph"
    :execution-id="$route.params.execution_id"
    :return-to="$route.query.return_to"
    :title="t('graph.build.resultTitle')"
    :read="buildAPI.getExecution"
    v-slot="{ execution }"
  >
    <el-descriptions v-if="execution.build_result" border :column="3">
      <el-descriptions-item :label="t('graph.build.totalMaterials')">{{ execution.build_result.total_materials }}</el-descriptions-item>
      <el-descriptions-item :label="t('graph.build.autoWritten')">{{ execution.build_result.auto_written }}</el-descriptions-item>
      <el-descriptions-item :label="t('graph.build.pendingReview')">{{ execution.build_result.pending_review }}</el-descriptions-item>
    </el-descriptions>
    <p v-else>{{ t('common.executionMonitor.emptyResult') }}</p>
  </ExecutionResultPage>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ExecutionResultPage } from '@common-ui'
import { buildAPI } from '../api/graphBuild'

const { t } = useI18n()
</script>
