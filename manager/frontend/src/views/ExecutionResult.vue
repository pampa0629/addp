<template>
  <ExecutionResultPage
    module="manager"
    :execution-id="$route.params.execution_id"
    :return-to="$route.query.return_to"
    :read="quickViewAPI.getExecutionStatus" :title="t('manager.executionResult.title')"
    v-slot="{ execution }"
  >
    <el-descriptions v-if="Object.keys(execution.metadata || {}).length" border :column="2">
      <el-descriptions-item v-for="(value, key) in execution.metadata" :key="key" :label="t('manager.executionResult.' + key)">
        <details v-if="typeof value === 'object'">
          <summary>{{ t('manager.executionResult.' + key) }}</summary>
          <pre>{{ JSON.stringify(value, null, 2) }}</pre>
        </details>
        <span v-else>{{ value }}</span>
      </el-descriptions-item>
    </el-descriptions>
    <p v-else>{{ t('common.executionMonitor.emptyResult') }}</p>
  </ExecutionResultPage>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ExecutionResultPage } from '@common-ui'
import { quickViewAPI } from '../api/quickView'
const { t } = useI18n()
</script>

<style scoped>
pre { white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
