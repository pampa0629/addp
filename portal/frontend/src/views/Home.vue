<template>
  <div class="home" v-loading="loading">
    <!-- 欢迎横幅 -->
    <div class="welcome-banner">
      <div class="banner-text">
        <h2 class="banner-title">资产门户</h2>
        <p class="banner-subtitle">
          共有 <strong>{{ totalPublished }}</strong> 个已上架数据资产，欢迎探索和使用
        </p>
      </div>
    </div>

    <!-- 资产类型快捷入口 -->
    <section class="section" v-if="typeStats.length">
      <h3 class="section-title">资产类型</h3>
      <div class="type-grid">
        <div
          v-for="stat in typeStats"
          :key="stat.type_code"
          class="type-card"
          @click="$router.push({ path: '/portal/search', query: { type_id: stat.type_id } })"
        >
          <el-icon class="type-icon"><component :is="getTypeIcon(stat.type_code)" /></el-icon>
          <div class="type-name">{{ stat.type_name }}</div>
          <div class="type-count">{{ stat.count }} 个</div>
        </div>
      </div>
    </section>

    <!-- 最新上架 -->
    <section class="section">
      <div class="section-header">
        <h3 class="section-title">最新上架</h3>
        <el-button text type="primary" @click="$router.push('/portal/search')">
          查看全部 →
        </el-button>
      </div>
      <el-empty v-if="!loading && latestAssets.length === 0" description="暂无已上架资产" />
      <el-row :gutter="16" v-else>
        <el-col
          v-for="asset in latestAssets"
          :key="asset.id"
          :xs="24" :sm="12" :md="8"
          class="asset-col"
        >
          <asset-card :asset="asset" />
        </el-col>
      </el-row>
    </section>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import {
  DataLine, Connection, TrendCharts, Document, Tools, Grid
} from '@element-plus/icons-vue'
import { homeAPI } from '../api/portal'
import AssetCard from '../components/AssetCard.vue'

const loading = ref(false)
const latestAssets = ref([])
const typeStats = ref([])
const totalPublished = ref(0)

// 类型图标映射
const typeIconMap = {
  dataset: Grid,
  table: Grid,
  service: Connection,
  metric: TrendCharts,
  standard: Document,
  develop: Tools,
  default: DataLine
}

function getTypeIcon(typeCode) {
  return typeIconMap[typeCode] || typeIconMap.default
}

async function fetchData() {
  loading.value = true
  try {
    const data = await homeAPI.getData()
    latestAssets.value = data.latest_assets || []
    typeStats.value = data.type_stats || []
    totalPublished.value = data.total_published || 0
  } catch (err) {
    console.error('获取首页数据失败:', err)
  } finally {
    loading.value = false
  }
}

onMounted(fetchData)
</script>

<style scoped>
.home {
  padding: 24px;
  max-width: 1200px;
  margin: 0 auto;
}

.welcome-banner {
  background: linear-gradient(135deg, var(--el-color-primary-light-8), var(--el-color-primary-light-9));
  border-radius: 8px;
  padding: 32px;
  margin-bottom: 32px;
  border: 1px solid var(--el-color-primary-light-7);
}

.banner-title {
  font-size: 24px;
  font-weight: 700;
  margin: 0 0 8px;
  color: var(--el-text-color-primary);
}

.banner-subtitle {
  font-size: 15px;
  color: var(--el-text-color-regular);
  margin: 0;
}

.section {
  margin-bottom: 32px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  margin: 0 0 16px;
  color: var(--el-text-color-primary);
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.section-header .section-title {
  margin: 0;
}

.type-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.type-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 110px;
  height: 90px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
}

.type-card:hover {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  transform: translateY(-2px);
}

.type-icon {
  font-size: 24px;
  color: var(--el-color-primary);
}

.type-name {
  font-size: 13px;
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.type-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.asset-col {
  margin-bottom: 16px;
}
</style>
