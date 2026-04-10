<template>
  <div class="dashboard" v-loading="loading">
    <!-- 资产状态汇总 -->
    <div class="section-title">{{ t('asset.dashboard.assetOverview') }}</div>
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

    <!-- 申请与授权汇总 -->
    <div class="section-title" style="margin-top: 28px">{{ t('asset.dashboard.applicationAndAuth') }}</div>
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
        <div class="stat-value">{{ stats.authorization_active }}</div>
        <div class="stat-label">{{ t('asset.dashboard.activeAuth') }}</div>
      </div>
      <div class="stat-card rating">
        <div class="stat-value">{{ stats.rating_count }}</div>
        <div class="stat-label">
          {{ t('asset.dashboard.ratingCount') }}
          <span v-if="stats.rating_count > 0" class="avg-score">
            (均 {{ stats.rating_avg_score.toFixed(1) }} 分)
          </span>
        </div>
      </div>
    </div>

    <!-- 趋势图区域 -->
    <div class="trends-section">
      <div class="trend-panel">
        <div class="trend-title">{{ t('asset.dashboard.publishTrend') }}</div>
        <div class="trend-chart" v-if="publishTrendData.length > 0">
          <div class="bar-chart">
            <div
              v-for="item in publishTrendData"
              :key="item.date"
              class="bar-item"
            >
              <div class="bar-wrapper">
                <div
                  class="bar published"
                  :style="{ height: barHeight(item.count, maxPublish) }"
                  :title="`${item.date}: ${item.count} 个`"
                ></div>
              </div>
              <div class="bar-label">{{ shortDate(item.date) }}</div>
            </div>
          </div>
        </div>
        <el-empty v-else :description="t('asset.dashboard.noPublishRecord')" :image-size="60" />
      </div>

      <div class="trend-panel">
        <div class="trend-title">{{ t('asset.dashboard.applicationTrend') }}</div>
        <div class="trend-chart" v-if="applicationTrendData.length > 0">
          <div class="bar-chart">
            <div
              v-for="item in applicationTrendData"
              :key="item.date"
              class="bar-item"
            >
              <div class="bar-wrapper">
                <div
                  class="bar application"
                  :style="{ height: barHeight(item.count, maxApplication) }"
                  :title="`${item.date}: ${item.count} 个`"
                ></div>
              </div>
              <div class="bar-label">{{ shortDate(item.date) }}</div>
            </div>
          </div>
        </div>
        <el-empty v-else :description="t('asset.dashboard.noApplicationRecord')" :image-size="60" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { statsAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const loading = ref(false)
const stats = ref({
  asset_total: 0,
  asset_draft: 0,
  asset_published: 0,
  asset_offline: 0,
  application_total: 0,
  application_pending: 0,
  authorization_active: 0,
  publish_trend: [],
  application_trend: [],
  rating_count: 0,
  rating_avg_score: 0
})

// 将后端返回的趋势数据补全为近 30 天（缺失日期补 0）
function fillTrend(trend) {
  const map = {}
  ;(trend || []).forEach(item => { map[item.date] = item.count })
  const result = []
  for (let i = 29; i >= 0; i--) {
    const d = new Date()
    d.setDate(d.getDate() - i)
    const date = d.toISOString().slice(0, 10)
    result.push({ date, count: map[date] || 0 })
  }
  return result
}

const publishTrendData = computed(() => fillTrend(stats.value.publish_trend))
const applicationTrendData = computed(() => fillTrend(stats.value.application_trend))

const maxPublish = computed(() => Math.max(...publishTrendData.value.map(i => i.count), 1))
const maxApplication = computed(() => Math.max(...applicationTrendData.value.map(i => i.count), 1))

function barHeight(count, max) {
  const pct = Math.round((count / max) * 100)
  return Math.max(pct, count > 0 ? 4 : 0) + '%'
}

function shortDate(dateStr) {
  // yyyy-MM-dd → MM/dd
  return dateStr.slice(5)
}

async function fetchStats() {
  loading.value = true
  try {
    const data = await statsAPI.dashboard()
    stats.value = data
  } catch (err) {
    ElMessage.error(t('asset.dashboard.fetchFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(fetchStats)
</script>

<style scoped>
.dashboard {
  padding: 24px;
}

.section-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 14px;
}

.stat-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.stat-card {
  background: var(--el-bg-color);
  border-radius: 10px;
  padding: 20px 24px;
  border: 1px solid var(--el-border-color-lighter);
  text-align: center;
  transition: box-shadow 0.2s;
}

.stat-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
  line-height: 1.2;
  margin-bottom: 6px;
}

.stat-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.avg-score {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.stat-card.total .stat-value   { color: var(--el-color-primary); }
.stat-card.draft .stat-value   { color: var(--el-color-warning); }
.stat-card.published .stat-value { color: var(--el-color-success); }
.stat-card.offline .stat-value { color: var(--el-text-color-placeholder); }
.stat-card.rating .stat-value  { color: #f7ba2a; }

/* 趋势图 */
.trends-section {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin-top: 28px;
}

.trend-panel {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 20px;
}

.trend-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
}

.trend-chart {
  height: 140px;
  display: flex;
  align-items: flex-end;
}

.bar-chart {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  width: 100%;
  height: 100%;
}

.bar-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  height: 100%;
}

.bar-wrapper {
  flex: 1;
  display: flex;
  align-items: flex-end;
  width: 100%;
}

.bar {
  width: 100%;
  border-radius: 3px 3px 0 0;
  min-height: 0;
  transition: height 0.3s;
  cursor: pointer;
}

.bar.published {
  background: var(--el-color-success);
  opacity: 0.7;
}

.bar.application {
  background: var(--el-color-primary);
  opacity: 0.7;
}

.bar-label {
  font-size: 9px;
  color: var(--el-text-color-placeholder);
  margin-top: 3px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  width: 100%;
  text-align: center;
}
</style>
