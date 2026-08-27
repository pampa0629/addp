<template>
  <div
    class="page-container"
    data-testid="catalog-governance-coverage"
    :data-load-state="loading ? 'loading' : error ? 'error' : coverage ? 'loaded' : 'idle'"
    :data-total-entries="coverage?.total_entries ?? ''"
  >
    <div class="page-header">
      <div>
        <h1>{{ t('catalog.coverage.title') }}</h1>
        <p>{{ t('catalog.coverage.description') }}</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="loadCoverage">{{ t('catalog.common.refresh') }}</el-button>
    </div>

    <el-alert type="info" :closable="false" show-icon :title="t('catalog.coverage.dynamicHint')" class="coverage-hint" />
    <el-skeleton v-if="loading && !coverage" :rows="7" animated />
    <el-result v-else-if="error" icon="error" :title="t('catalog.coverage.loadFailed')" :sub-title="error">
      <template #extra><el-button type="primary" @click="loadCoverage">{{ t('catalog.common.retry') }}</el-button></template>
    </el-result>
    <template v-else-if="coverage">
      <el-row :gutter="16" class="status-grid">
        <el-col :xs="12" :sm="8" :lg="4">
          <el-card shadow="never"><el-statistic :title="t('catalog.coverage.totalEntries')" :value="coverage.total_entries" /></el-card>
        </el-col>
        <el-col v-for="status in coverage.governance_statuses" :key="status.status" :xs="12" :sm="8" :lg="4">
          <el-card shadow="never">
            <el-statistic :title="t(`catalog.status.governance.${status.status}`)" :value="status.count" />
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never">
        <template #header><strong>{{ t('catalog.coverage.dimensionTitle') }}</strong></template>
        <el-table :data="coverage.dimensions">
          <el-table-column :label="t('catalog.coverage.dimension')" min-width="210">
            <template #default="{ row }">
              <div class="dimension-name" data-testid="catalog-coverage-dimension" :data-dimension-key="row.key">
                {{ coverageDimensionLabel(t, row.key, 'name') }}
              </div>
              <div class="dimension-description">{{ coverageDimensionLabel(t, row.key, 'description') }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('catalog.coverage.rate')" min-width="230">
            <template #default="{ row }">
              <el-progress :percentage="row.coverage_rate" :stroke-width="12" />
            </template>
          </el-table-column>
          <el-table-column prop="covered" :label="t('catalog.coverage.covered')" width="110" />
          <el-table-column :label="t('catalog.coverage.notCovered')" width="120">
            <template #default="{ row }">
              <el-button
                v-if="row.not_covered > 0"
                link
                type="primary"
                data-testid="catalog-coverage-missing-link"
                :aria-label="t('catalog.coverage.openMissing', { dimension: coverageDimensionLabel(t, row.key, 'name'), count: row.not_covered })"
                @click="openMissingEntries(row)"
              >
                {{ row.not_covered }}
              </el-button>
              <span v-else>0</span>
            </template>
          </el-table-column>
          <el-table-column prop="applicable" :label="t('catalog.coverage.applicable')" width="110" />
          <el-table-column prop="not_applicable" :label="t('catalog.coverage.notApplicable')" width="120" />
        </el-table>
      </el-card>
    </template>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { navigateConsoleModuleRoute, useConsolePageDescriptor } from '@common-ui'
import { getGovernanceCoverage } from '../api/catalog'
import { buildMissingCoverageEntryQuery, coverageDimensionLabel } from '../utils/governanceCoverageView'

const { t } = useI18n()
const router = useRouter()
const coverage = ref(null)
const loading = ref(false)
const error = ref('')

useConsolePageDescriptor(router, 'catalog', {
  title: t('catalog.coverage.title'),
  subject: t('catalog.coverage.recentSubject'),
  ready: true
})

async function loadCoverage() {
  loading.value = true
  error.value = ''
  try {
    coverage.value = await getGovernanceCoverage()
  } catch (requestError) {
    error.value = requestError?.response?.data?.error || t('catalog.coverage.loadFailed')
  } finally {
    loading.value = false
  }
}

async function openMissingEntries(dimension) {
  if (!dimension || dimension.not_covered <= 0) return
  const query = buildMissingCoverageEntryQuery(dimension.key)
  if (!query) return
  await navigateConsoleModuleRoute(router, 'catalog', { path: '/entries', query })
}

onMounted(loadCoverage)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.page-header h1 { margin: 0; color: var(--addp-text-primary); font-size: 24px; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.coverage-hint, .status-grid { margin-bottom: 16px; }
.status-grid { row-gap: 16px; }
.dimension-name { color: var(--addp-text-primary); font-weight: 600; }
.dimension-description { color: var(--addp-text-secondary); font-size: 12px; margin-top: 4px; line-height: 1.5; }
@media (max-width: 760px) {
  .page-header { flex-direction: column; }
}
</style>
