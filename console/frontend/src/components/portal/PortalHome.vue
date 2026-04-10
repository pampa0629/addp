<template>
  <div class="home-view">
    <!-- 欢迎区 -->
    <div class="home-header">
      <div class="welcome-text">
        <h2>{{ t('console.welcome.greeting', { name: user?.username || t('console.welcome.defaultName') }) }}</h2>
        <p>{{ t('console.welcome.subtitle') }}</p>
      </div>
    </div>

    <!-- 状态快照 -->
    <div class="status-snapshot">
      <div
        v-for="stat in statusStats"
        :key="stat.key"
        class="stat-item"
        :class="{ 'is-loading': stat.loading }"
      >
        <el-icon :size="20" :style="{ color: stat.color }"><component :is="stat.icon" /></el-icon>
        <div class="stat-info">
          <span class="stat-value">{{ stat.loading ? '…' : (stat.value ?? '-') }}</span>
          <span class="stat-label">{{ t(stat.label) }}</span>
        </div>
      </div>
    </div>

    <!-- 建议下一步 -->
    <div class="section">
      <h3 class="section-title">{{ t('console.home.nextSteps') }}</h3>
      <div class="scenario-grid">
        <div
          v-for="scenario in recommendedScenarios"
          :key="scenario.key"
          class="scenario-card"
          @click="handleScenarioClick(scenario)"
        >
          <div class="scenario-icon" :style="{ background: scenario.bgColor }">
            <el-icon :size="24" :style="{ color: scenario.color }"><component :is="scenario.icon" /></el-icon>
          </div>
          <div class="scenario-body">
            <h4>{{ t(scenario.title) }}</h4>
            <p>{{ t(scenario.desc) }}</p>
            <div class="scenario-path">
              <span v-for="(step, i) in scenario.path" :key="i">
                <span class="path-step" @click.stop="navigateToModule(step.module)">{{ t(step.label) }}</span>
                <el-icon v-if="i < scenario.path.length - 1" :size="10" class="path-arrow"><ArrowRight /></el-icon>
              </span>
            </div>
          </div>
          <el-button type="primary" size="small" class="scenario-btn">{{ t('console.home.start') }}</el-button>
        </div>
      </div>
    </div>

    <!-- 最近访问 -->
    <div v-if="recentVisits.length > 0" class="section">
      <h3 class="section-title">{{ t('console.home.recentVisits') }}</h3>
      <div class="recent-list">
        <div
          v-for="item in recentVisits"
          :key="item.key"
          class="recent-item"
          @click="handleRecentClick(item)"
        >
          <el-icon :size="14"><component :is="item.icon" /></el-icon>
          <span>{{ t(item.label) }}</span>
        </div>
      </div>
    </div>

    <!-- 所有模块（折叠） -->
    <div class="section">
      <div class="section-header" @click="allModulesExpanded = !allModulesExpanded">
        <h3 class="section-title">{{ t('console.home.allModules') }}</h3>
        <el-icon class="expand-icon" :class="{ 'is-expanded': allModulesExpanded }"><ArrowDown /></el-icon>
      </div>
      <div v-if="allModulesExpanded" class="cards-grid">
        <el-card
          shadow="hover"
          class="module-card"
          @click="$emit('portal-click')"
        >
          <div class="card-content">
            <el-icon :size="36" style="color: var(--addp-module-portal)"><DataBoard /></el-icon>
            <h4>{{ t('console.modules.portal.label') }}</h4>
            <p>{{ t('console.modules.portal.desc') }}</p>
          </div>
        </el-card>
        <el-card
          v-for="card in homeCards"
          :key="card.module"
          shadow="hover"
          class="module-card"
          @click="$emit('card-click', card.module)"
        >
          <div class="card-content">
            <el-icon :size="36" :style="{ color: `var(${card.cssVar})` }">
              <component :is="card.icon" />
            </el-icon>
            <h4>{{ t(card.label) }}</h4>
            <p>{{ t(card.desc) }}</p>
          </div>
        </el-card>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  DataBoard, ArrowDown, ArrowRight,
  Upload, Box, DataAnalysis, Link, Edit, Share, CircleCheck, Operation,
  Connection,
} from '@element-plus/icons-vue'
import client from '../../api/client'

const { t } = useI18n()

const props = defineProps({
  activeGroup: { type: String, default: null },
  homeCards: { type: Array, required: true },
  user: { type: Object, default: null },
})

const emit = defineEmits(['card-click', 'portal-click', 'navigate'])

// ─── 状态快照 ────────────────────────────────────────────────────────────────

const statusData = ref({ engines: null, datasets: null, services: null, tasks: null })
const statusLoading = ref(true)

const statusStats = computed(() => [
  { key: 'engines',  icon: Connection,   color: 'var(--addp-module-system)',   label: 'console.home.stats.engines',  value: statusData.value.engines,  loading: statusLoading.value },
  { key: 'datasets', icon: Box,          color: 'var(--addp-module-meta)',     label: 'console.home.stats.datasets', value: statusData.value.datasets, loading: statusLoading.value },
  { key: 'services', icon: Link,         color: 'var(--addp-module-service)',  label: 'console.home.stats.services', value: statusData.value.services, loading: statusLoading.value },
  { key: 'tasks',    icon: Operation,    color: 'var(--addp-module-orchestrator)', label: 'console.home.stats.runningTasks', value: statusData.value.tasks, loading: statusLoading.value },
])

async function fetchStatus() {
  statusLoading.value = true
  const get = (url) => client.get(url).then(r => r?.data?.total ?? r?.total ?? 0).catch(() => null)

  const [engines, datasets, services, tasks] = await Promise.allSettled([
    get('/system/engines?page_size=1'),
    get('/meta/stats'),
    get('/service/query?page_size=1'),
    get('/monitor/executions?status=running&page_size=1'),
  ])

  statusData.value = {
    engines:  engines.status  === 'fulfilled' ? engines.value  : null,
    datasets: datasets.status === 'fulfilled' ? datasets.value : null,
    services: services.status === 'fulfilled' ? services.value : null,
    tasks:    tasks.status    === 'fulfilled' ? tasks.value    : null,
  }
  statusLoading.value = false
}

// ─── 状态推断 ────────────────────────────────────────────────────────────────

const platformStage = computed(() => {
  const { engines, datasets, services } = statusData.value
  if (statusLoading.value) return 'loading'
  if (engines === 0 || engines === null) return 'uninitialized'
  if (datasets === 0 || datasets === null) return 'ready'
  if (services === 0 || services === null) return 'has-data'
  return 'active'
})

// ─── 场景定义 ────────────────────────────────────────────────────────────────

const ALL_SCENARIOS = [
  {
    key: 'ingest',
    title: 'console.home.scenarios.ingest.title',
    desc: 'console.home.scenarios.ingest.desc',
    icon: Upload,
    color: 'var(--addp-module-transfer)',
    bgColor: 'color-mix(in srgb, var(--addp-module-transfer) 12%, transparent)',
    path: [
      { module: 'system', label: 'console.modules.system.label' },
      { module: 'transfer', label: 'console.modules.transfer.label' },
      { module: 'meta', label: 'console.modules.meta.label' },
    ],
    firstModule: 'system',
    stages: ['uninitialized', 'ready'],
  },
  {
    key: 'service',
    title: 'console.home.scenarios.service.title',
    desc: 'console.home.scenarios.service.desc',
    icon: Link,
    color: 'var(--addp-module-service)',
    bgColor: 'color-mix(in srgb, var(--addp-module-service) 12%, transparent)',
    path: [
      { module: 'manager', label: 'console.modules.manager.label' },
      { module: 'service', label: 'console.modules.service.label' },
    ],
    firstModule: 'manager',
    stages: ['has-data', 'active'],
  },
  {
    key: 'develop',
    title: 'console.home.scenarios.develop.title',
    desc: 'console.home.scenarios.develop.desc',
    icon: Edit,
    color: 'var(--addp-module-develop)',
    bgColor: 'color-mix(in srgb, var(--addp-module-develop) 12%, transparent)',
    path: [
      { module: 'develop', label: 'console.modules.develop.label' },
      { module: 'orchestrator', label: 'console.modules.orchestrator.label' },
    ],
    firstModule: 'develop',
    stages: ['has-data', 'active'],
  },
  {
    key: 'govern',
    title: 'console.home.scenarios.govern.title',
    desc: 'console.home.scenarios.govern.desc',
    icon: CircleCheck,
    color: 'var(--addp-module-quality)',
    bgColor: 'color-mix(in srgb, var(--addp-module-quality) 12%, transparent)',
    path: [
      { module: 'standard', label: 'console.modules.standard.label' },
      { module: 'quality', label: 'console.modules.quality.label' },
    ],
    firstModule: 'standard',
    stages: ['has-data', 'active'],
  },
  {
    key: 'graph',
    title: 'console.home.scenarios.graph.title',
    desc: 'console.home.scenarios.graph.desc',
    icon: Share,
    color: 'var(--addp-module-monitor)',
    bgColor: 'color-mix(in srgb, var(--addp-module-monitor) 12%, transparent)',
    path: [
      { module: 'graph', label: 'console.modules.graph.label' },
      { module: 'service', label: 'console.modules.service.label' },
    ],
    firstModule: 'graph',
    stages: ['active'],
  },
  {
    key: 'monitor',
    title: 'console.home.scenarios.monitor.title',
    desc: 'console.home.scenarios.monitor.desc',
    icon: DataAnalysis,
    color: 'var(--addp-module-monitor)',
    bgColor: 'color-mix(in srgb, var(--addp-module-monitor) 12%, transparent)',
    path: [
      { module: 'monitor', label: 'console.modules.monitor.label' },
    ],
    firstModule: 'monitor',
    stages: ['active'],
  },
]

const recommendedScenarios = computed(() => {
  const stage = platformStage.value
  if (stage === 'loading') return []
  const matched = ALL_SCENARIOS.filter(s => s.stages.includes(stage))
  return matched.slice(0, 3)
})

// ─── 最近访问 ────────────────────────────────────────────────────────────────

const RECENT_KEY = 'addp_recent_visits'
const recentVisitsRaw = ref([])

function loadRecentVisits() {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    recentVisitsRaw.value = raw ? JSON.parse(raw) : []
  } catch {
    recentVisitsRaw.value = []
  }
}

const recentVisits = computed(() =>
  recentVisitsRaw.value.map(item => {
    const card = props.homeCards.find(c => c.module === item.module)
    return { ...item, icon: card?.icon || DataBoard }
  })
)

function handleRecentClick(item) {
  emit('navigate', item.route)
}

// ─── 折叠所有模块 ────────────────────────────────────────────────────────────

const allModulesExpanded = ref(false)

// ─── 场景点击 ────────────────────────────────────────────────────────────────

function handleScenarioClick(scenario) {
  emit('card-click', scenario.firstModule)
}

function navigateToModule(module) {
  emit('card-click', module)
}

// ─── 初始化 ──────────────────────────────────────────────────────────────────

onMounted(() => {
  fetchStatus()
  loadRecentVisits()
})
</script>

<style scoped>
.home-view {
  padding: 32px 40px;
  overflow-y: auto;
  height: 100%;
  box-sizing: border-box;
}

/* 欢迎区 */
.home-header {
  margin-bottom: 24px;
}

.welcome-text h2 {
  font-size: 22px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0 0 6px 0;
}

.welcome-text p {
  color: var(--addp-text-tertiary);
  font-size: 14px;
  margin: 0;
}

/* 状态快照 */
.status-snapshot {
  display: flex;
  gap: 16px;
  margin-bottom: 32px;
  flex-wrap: wrap;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 10px;
  padding: 12px 20px;
  flex: 1;
  min-width: 140px;
}

.stat-info {
  display: flex;
  flex-direction: column;
}

.stat-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--addp-text-primary);
  line-height: 1.2;
}

.stat-label {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 2px;
}

/* 区块 */
.section {
  margin-bottom: 28px;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
  padding: 4px 0;
}

.section-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--addp-text-secondary);
  margin: 0 0 14px 0;
}

.section-header .section-title {
  margin-bottom: 0;
}

.expand-icon {
  color: var(--addp-text-tertiary);
  transition: transform 0.2s;
}

.expand-icon.is-expanded {
  transform: rotate(180deg);
}

/* 场景卡片 */
.scenario-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}

.scenario-card {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 12px;
  padding: 16px;
  cursor: pointer;
  transition: box-shadow 0.2s, border-color 0.2s;
  position: relative;
}

.scenario-card:hover {
  box-shadow: var(--addp-shadow-hover);
  border-color: var(--el-color-primary-light-5);
}

.scenario-icon {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.scenario-body {
  flex: 1;
  min-width: 0;
}

.scenario-body h4 {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0 0 4px 0;
}

.scenario-body p {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin: 0 0 8px 0;
  line-height: 1.5;
}

.scenario-path {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
}

.path-step {
  font-size: 11px;
  color: var(--el-color-primary);
  cursor: pointer;
  padding: 1px 4px;
  border-radius: 4px;
  transition: background 0.15s;
}

.path-step:hover {
  background: var(--el-color-primary-light-9);
}

.path-arrow {
  color: var(--addp-text-tertiary);
  margin: 0 1px;
}

.scenario-btn {
  flex-shrink: 0;
  align-self: center;
}

/* 最近访问 */
.recent-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.recent-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 20px;
  cursor: pointer;
  font-size: 13px;
  color: var(--addp-text-secondary);
  transition: border-color 0.2s, color 0.2s;
}

.recent-item:hover {
  border-color: var(--el-color-primary-light-5);
  color: var(--el-color-primary);
}

/* 所有模块折叠区 */
.cards-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
  margin-top: 14px;
}

.module-card {
  cursor: pointer;
  transition: all 0.2s;
}

.module-card:hover {
  transform: var(--addp-card-hover-transform);
  box-shadow: var(--addp-shadow-hover);
}

.card-content {
  text-align: center;
  padding: 16px 8px;
}

.card-content h4 {
  margin: 10px 0 6px 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.card-content p {
  color: var(--addp-text-tertiary);
  font-size: 12px;
  margin: 0;
  line-height: 1.4;
}
</style>
