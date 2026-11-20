<template>
  <ExecutionResultPage
    module="orchestrator"
    :execution-id="$route.params.execution_id"
    :return-to="$route.query.return_to"
    :read="orchestrationAPI.getExecutionByUUID" :title="t('orchestrator.executionList.stepResults')"
    v-slot="{ execution }"
  >
    <OrchestrationExecutionResult :current-execution="execution" />
  </ExecutionResultPage>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { ExecutionResultPage } from '@common-ui'
import OrchestrationExecutionResult from '../components/OrchestrationExecutionResult.vue'
import orchestrationAPI from '../api/orchestration'
const { t } = useI18n()
</script>
