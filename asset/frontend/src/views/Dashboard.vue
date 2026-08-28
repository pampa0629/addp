<template>
  <main class="dashboard" v-loading="loading">
    <StatusAnnouncer
      :message="loading ? t('asset.dashboard.loading') : loadAnnouncement"
      :label="t('asset.dashboard.announcementLabel')"
    />

    <header class="dashboard-header">
      <div>
        <h1 class="dashboard-title">{{ t('asset.dashboard.title') }}</h1>
        <p class="dashboard-description">{{ t('asset.dashboard.description') }}</p>
      </div>
      <div class="scope-control">
        <label class="scope-label" for="asset-dashboard-scope">{{ t('asset.dashboard.scope') }}</label>
        <el-select
          id="asset-dashboard-scope"
          v-model="selectedScope"
          class="scope-select"
          filterable
          :loading="assetOptionsLoading"
          :placeholder="t('asset.dashboard.scopePlaceholder')"
        >
          <el-option :label="t('asset.dashboard.scopeAll')" :value="DASHBOARD_SCOPE_ALL" />
          <el-option :label="t('asset.dashboard.scopeApplications')" :value="DASHBOARD_SCOPE_APPLICATION" />
          <el-option-group
            v-if="applicationAssets.length > 0"
            :label="t('asset.dashboard.scopeSpecificApplications')"
          >
            <el-option
              v-for="asset in applicationAssets"
              :key="asset.id"
              :label="asset.name"
              :value="`${DASHBOARD_SCOPE_ASSET_PREFIX}${asset.id}`"
            />
          </el-option-group>
        </el-select>
      </div>
    </header>

    <p class="scope-hint">
      {{ t('asset.dashboard.currentScope', { scope: selectedScopeLabel }) }}
      · {{ t('asset.dashboard.assetOwnedFactsOnly') }}
    </p>

    <section aria-labelledby="asset-overview-heading">
      <h2 id="asset-overview-heading" class="section-title">{{ t('asset.dashboard.assetOverview') }}</h2>
      <div class="stat-cards">
        <div class="stat-card total">
          <div class="stat-value">{{ stats.asset_total }}</div>
          <div class="stat-label">{{ t('asset.dashboard.totalAssets') }}</div>
        </div>
        <div class="stat-card draft">
          <div class="stat-value">{{ stats.asset_draft }}</div>
          <div class="stat-label">{{ t('asset.dashboard.draft') }}</div>
        </div>
        <div class="stat-card published">
          <div class="stat-value">{{ stats.asset_published }}</div>
          <div class="stat-label">{{ t('asset.dashboard.published') }}</div>
        </div>
        <div class="stat-card offline">
          <div class="stat-value">{{ stats.asset_offline }}</div>
          <div class="stat-label">{{ t('asset.dashboard.offline') }}</div>
        </div>
      </div>
    </section>

    <section class="dashboard-section" aria-labelledby="application-results-heading">
      <h2 id="application-results-heading" class="section-title">{{ t('asset.dashboard.applicationResults') }}</h2>
      <div class="stat-cards">
        <div class="stat-card total">
          <div class="stat-value">{{ stats.application_total }}</div>
          <div class="stat-label">{{ t('asset.dashboard.totalApplications') }}</div>
        </div>
        <div class="stat-card draft">
          <div class="stat-value">{{ stats.application_pending }}</div>
          <div class="stat-label">{{ t('asset.dashboard.pending') }}</div>
        </div>
        <div class="stat-card published">
          <div class="stat-value">{{ stats.application_approved }}</div>
          <div class="stat-label">{{ t('asset.dashboard.approved') }}</div>
        </div>
        <div class="stat-card rejected">
          <div class="stat-value">{{ stats.application_rejected }}</div>
          <div class="stat-label">{{ t('asset.dashboard.rejected') }}</div>
        </div>
      </div>
    </section>

    <section class="dashboard-section" aria-labelledby="authorization-rating-heading">
      <h2 id="authorization-rating-heading" class="section-title">{{ t('asset.dashboard.authorizationAndRating') }}</h2>
      <div class="stat-cards stat-cards--three">
        <div class="stat-card published">
          <div class="stat-value">{{ stats.effective_authorized_users }}</div>
          <div class="stat-label">{{ t('asset.dashboard.effectiveAuthorizedUsers') }}</div>
          <div class="stat-help">{{ t('asset.dashboard.effectiveAuthorizedUsersHint') }}</div>
        </div>
        <div class="stat-card rating">
          <div class="stat-value">{{ stats.rating_count }}</div>
          <div class="stat-label">{{ t('asset.dashboard.ratingCount') }}</div>
        </div>
        <div class="stat-card rating">
          <div class="stat-value">{{ ratingAverage }}</div>
          <div class="stat-label">{{ t('asset.dashboard.ratingAverage') }}</div>
        </div>
      </div>
    </section>

    <section class="trends-section" :aria-label="t('asset.dashboard.trends')">
      <div class="trend-panel">
        <h2 class="trend-title">{{ t('asset.dashboard.publishTrend') }}</h2>
        <div v-if="hasPublishRecords" class="trend-chart">
          <div class="bar-chart">
            <div v-for="item in publishTrendData" :key="item.date" class="bar-item">
              <div class="bar-wrapper">
                <div
                  class="bar published"
                  role="img"
                  :style="{ height: barHeight(item.count, maxPublish) }"
                  :title="t('asset.dashboard.trendValue', { date: item.date, count: item.count })"
                  :aria-label="t('asset.dashboard.trendValue', { date: item.date, count: item.count })"
                />
              </div>
              <div class="bar-label">{{ shortDate(item.date) }}</div>
            </div>
          </div>
        </div>
        <el-empty v-else :description="t('asset.dashboard.noPublishRecord')" :image-size="60" />
      </div>

      <div class="trend-panel">
        <h2 class="trend-title">{{ t('asset.dashboard.applicationTrend') }}</h2>
        <div v-if="hasApplicationRecords" class="trend-chart">
          <div class="bar-chart">
            <div v-for="item in applicationTrendData" :key="item.date" class="bar-item">
              <div class="bar-wrapper">
                <div
                  class="bar application"
                  role="img"
                  :style="{ height: barHeight(item.count, maxApplication) }"
                  :title="t('asset.dashboard.trendValue', { date: item.date, count: item.count })"
                  :aria-label="t('asset.dashboard.trendValue', { date: item.date, count: item.count })"
                />
              </div>
              <div class="bar-label">{{ shortDate(item.date) }}</div>
            </div>
          </div>
        </div>
        <el-empty v-else :description="t('asset.dashboard.noApplicationRecord')" :image-size="60" />
      </div>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { StatusAnnouncer } from '@common-ui'
import { assetAPI, statsAPI, typeDefinitionAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'
import {
  DASHBOARD_SCOPE_ALL,
  DASHBOARD_SCOPE_APPLICATION,
  DASHBOARD_SCOPE_ASSET_PREFIX,
  dashboardStatsParams
} from '../utils/dashboardScope'

const { t } = useI18n()

function emptyStats() {
  return {
    asset_total: 0,
    asset_draft: 0,
    asset_published: 0,
    asset_offline: 0,
    application_total: 0,
    application_pending: 0,
    application_approved: 0,
    application_rejected: 0,
    effective_authorized_users: 0,
    publish_trend: [],
    application_trend: [],
    rating_count: 0,
    rating_avg_score: 0
  }
}

const loading = ref(false)
const assetOptionsLoading = ref(false)
const loadAnnouncementKey = ref('')
const stats = ref(emptyStats())
const selectedScope = ref(DASHBOARD_SCOPE_APPLICATION)
const applicationAssets = ref([])
let statsRequestSequence = 0

const loadAnnouncement = computed(() => (
  loadAnnouncementKey.value
    ? t(`asset.dashboard.${loadAnnouncementKey.value}`)
    : ''
))

const selectedScopeLabel = computed(() => {
  if (selectedScope.value === DASHBOARD_SCOPE_ALL) return t('asset.dashboard.scopeAll')
  if (selectedScope.value === DASHBOARD_SCOPE_APPLICATION) return t('asset.dashboard.scopeApplications')
  const id = Number(selectedScope.value.slice(DASHBOARD_SCOPE_ASSET_PREFIX.length))
  return applicationAssets.value.find(asset => asset.id === id)?.name || t('asset.dashboard.scopeSpecificAsset')
})

function fillTrend(trend) {
  const countsByDate = new Map((trend || []).map(item => [item.date, item.count]))
  const result = []
  for (let i = 29; i >= 0; i--) {
    const date = new Date()
    date.setDate(date.getDate() - i)
    const key = date.toISOString().slice(0, 10)
    result.push({ date: key, count: countsByDate.get(key) || 0 })
  }
  return result
}

const publishTrendData = computed(() => fillTrend(stats.value.publish_trend))
const applicationTrendData = computed(() => fillTrend(stats.value.application_trend))
const hasPublishRecords = computed(() => publishTrendData.value.some(item => item.count > 0))
const hasApplicationRecords = computed(() => applicationTrendData.value.some(item => item.count > 0))
const maxPublish = computed(() => Math.max(...publishTrendData.value.map(item => item.count), 1))
const maxApplication = computed(() => Math.max(...applicationTrendData.value.map(item => item.count), 1))
const ratingAverage = computed(() => stats.value.rating_count > 0 ? stats.value.rating_avg_score.toFixed(1) : '—')

function barHeight(count, max) {
  const percentage = Math.round((count / max) * 100)
  return Math.max(percentage, count > 0 ? 4 : 0) + '%'
}

function shortDate(date) {
  return date.slice(5)
}

async function fetchStats() {
  const requestSequence = ++statsRequestSequence
  loading.value = true
  loadAnnouncementKey.value = ''
  try {
    const result = await statsAPI.dashboard(dashboardStatsParams(selectedScope.value))
    if (requestSequence !== statsRequestSequence) return
    stats.value = result
    loadAnnouncementKey.value = 'loaded'
  } catch {
    if (requestSequence !== statsRequestSequence) return
    stats.value = emptyStats()
    loadAnnouncementKey.value = 'fetchFailed'
    ElMessage.error(t('asset.dashboard.fetchFailed'))
  } finally {
    if (requestSequence === statsRequestSequence) loading.value = false
  }
}

async function loadApplicationAssets() {
  assetOptionsLoading.value = true
  try {
    const types = await typeDefinitionAPI.list()
    const applicationType = (types || []).find(type => type.code === 'application')
    if (!applicationType) {
      applicationAssets.value = []
      return
    }

    const assets = []
    let page = 1
    let total = 0
    do {
      const result = await assetAPI.list({ type_id: applicationType.id, page, page_size: 100 })
      const pageAssets = result.data || []
      assets.push(...pageAssets)
      total = result.total || 0
      page += 1
      if (pageAssets.length === 0) break
    } while (assets.length < total)
    applicationAssets.value = assets
  } catch {
    applicationAssets.value = []
    ElMessage.error(t('asset.dashboard.assetOptionsFailed'))
  } finally {
    assetOptionsLoading.value = false
  }
}

watch(selectedScope, fetchStats)
onMounted(() => {
  loadApplicationAssets()
  fetchStats()
})
</script>

<style scoped>
.dashboard {
  padding: 24px;
  color: var(--addp-text-primary);
}

.dashboard-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 8px;
}

.dashboard-title {
  margin: 0;
  font-size: 24px;
  line-height: 1.2;
}

.dashboard-description,
.scope-hint {
  margin: 8px 0 0;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.scope-hint {
  margin-bottom: 24px;
}

.scope-control {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: min(360px, 100%);
}

.scope-label {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.scope-select {
  width: 100%;
}

.dashboard-section {
  margin-top: 28px;
}

.section-title,
.trend-title {
  margin: 0 0 14px;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
}

.stat-cards {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.stat-cards--three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.stat-card,
.trend-panel {
  background: var(--addp-bg-primary) !important;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
}

.stat-card {
  padding: 20px 24px;
  text-align: center;
}

.stat-card:hover {
  box-shadow: var(--addp-shadow-hover);
}

.stat-value {
  margin-bottom: 6px;
  font-size: 32px;
  font-weight: 700;
  line-height: 1.2;
}

.stat-label {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.stat-help {
  margin-top: 6px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
}

.stat-card.total .stat-value {
  color: var(--el-color-primary);
}

.stat-card.draft .stat-value,
.stat-card.rating .stat-value {
  color: var(--el-color-warning);
}

.stat-card.published .stat-value {
  color: var(--el-color-success);
}

.stat-card.offline .stat-value {
  color: var(--addp-text-tertiary);
}

.stat-card.rejected .stat-value {
  color: var(--el-color-danger);
}

.trends-section {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  margin-top: 28px;
}

.trend-panel {
  padding: 20px;
}

.trend-chart {
  display: flex;
  align-items: flex-end;
  height: 140px;
}

.bar-chart {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  width: 100%;
  height: 100%;
}

.bar-item {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  height: 100%;
}

.bar-wrapper {
  display: flex;
  flex: 1;
  align-items: flex-end;
  width: 100%;
}

.bar {
  width: 100%;
  min-height: 0;
  border-radius: 3px 3px 0 0;
  cursor: default;
  opacity: 0.7;
  transition: height 0.3s;
}

.bar.published {
  background: var(--el-color-success);
}

.bar.application {
  background: var(--el-color-primary);
}

.bar-label {
  width: 100%;
  margin-top: 3px;
  overflow: hidden;
  color: var(--addp-text-tertiary);
  font-size: 9px;
  text-align: center;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1100px) {
  .stat-cards,
  .stat-cards--three {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 768px) {
  .dashboard {
    padding: 16px;
  }

  .dashboard-header {
    align-items: stretch;
    flex-direction: column;
  }

  .scope-control {
    min-width: 0;
  }

  .stat-cards,
  .stat-cards--three,
  .trends-section {
    grid-template-columns: 1fr;
  }
}
</style>
