<template>
  <ExecutionResultPage
    :key="page" module="security" :title="t('security.finding.executionTitle')"
    :execution-id="id" :return-to="route.query.return_to" :read="readResult"
    v-slot="{ execution }"
  >
    <p>{{ t('common.executionSteps.status') }} · {{ t(`common.executionSteps.states.${execution.status}`) }}</p>
    <el-table v-if="execution.total" :data="execution.findings" row-key="id">
      <el-table-column prop="component_key" :label="t('security.finding.observedComponent')" />
      <el-table-column prop="detector_code" :label="t('security.finding.detector')" />
      <el-table-column prop="detector_version" :label="t('security.finding.detectorVersion')" />
      <el-table-column :label="t('security.finding.confidence')">
        <template #default="{ row }">{{ percent.format(row.confidence) }}</template>
      </el-table-column>
      <el-table-column :label="t('security.finding.observedAt')">
        <template #default="{ row }">{{ new Date(row.observed_at).toLocaleString(locale) }}</template>
      </el-table-column>
    </el-table>
    <p v-else>{{ t(execution.status === 'success' && execution.metadata?.finding_count === 0 ? 'security.finding.zeroHits' : 'security.finding.noRecordedFindings') }}</p>
    <el-pagination v-if="execution.total > pageSize" v-model:current-page="page" :page-size="pageSize"
      :total="execution.total" layout="total, prev, pager, next" />
  </ExecutionResultPage>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ExecutionResultPage } from '@common-ui'
import client from '../api/client'
import { findingAPI } from '../api/security'

const route = useRoute()
const { t, locale } = useI18n()
const id = computed(() => route.params.id)
const page = ref(1)
const pageSize = 20
const percent = computed(() => new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 2 }))
watch(id, () => { page.value = 1 }, { flush: 'sync' })

async function readResult(executionId, options) {
  const selectedPage = page.value
  const observation = await client.get('/monitor/executions/by-execution-id/' + encodeURIComponent(executionId), options)
  if (observation.execution_id !== executionId || observation.module !== 'security' || observation.task_type !== 'sensitive_data_discovery') {
    throw new Error('discovery_execution_mismatch')
  }
  const result = await findingAPI.list({ discovery_execution_id: executionId, page: selectedPage, page_size: pageSize }, options)
  if (result.page !== selectedPage || !Number.isSafeInteger(result.total) || result.total < 0 ||
    !Array.isArray(result.data) || result.data.some(row => row.discovery_execution_id !== executionId)) {
    throw new Error('discovery_findings_mismatch')
  }
  return { ...observation, total: result.total, findings: result.data }
}
</script>
