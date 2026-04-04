<template>
  <div class="home-view">
    <div v-if="!activeGroup" class="home-welcome">
      <h2>欢迎使用全域数据平台</h2>
      <p>请从顶部选择功能区域，或直接点击模块卡片进入</p>
    </div>

    <div class="cards-grid">
      <!-- 数据门户卡片：全局首页或资产管理群组时显示 -->
      <el-card
        v-if="!activeGroup || activeGroup === 'asset'"
        shadow="hover"
        class="module-card"
        @click="$emit('portal-click')"
      >
        <div class="card-content">
          <el-icon :size="48" style="color: var(--addp-module-portal)"><DataBoard /></el-icon>
          <h2>数据门户</h2>
          <p>数据消费者门户，搜索与申请数据资产</p>
        </div>
      </el-card>

      <!-- 模块卡片（按当前群组过滤） -->
      <el-card
        v-for="card in homeCards"
        :key="card.module"
        shadow="hover"
        class="module-card"
        @click="$emit('card-click', card.module)"
      >
        <div class="card-content">
          <el-icon :size="48" :style="{ color: `var(${card.cssVar})` }">
            <component :is="card.icon" />
          </el-icon>
          <h2>{{ card.label }}</h2>
          <p>{{ card.desc }}</p>
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { DataBoard } from '@element-plus/icons-vue'

defineProps({
  activeGroup: { type: String, default: null },
  homeCards: { type: Array, required: true },
})

defineEmits(['card-click', 'portal-click'])
</script>

<style scoped>
.home-view {
  padding: 40px;
  overflow-y: auto;
}

.home-welcome {
  margin-bottom: 32px;
}

.home-welcome h2 {
  font-size: 22px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin-bottom: 8px;
}

.home-welcome p {
  color: var(--addp-text-tertiary);
  font-size: 14px;
}

.cards-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
}

.module-card {
  cursor: pointer;
  transition: all 0.3s;
  height: 200px;
}

.module-card:hover {
  transform: var(--addp-card-hover-transform);
  box-shadow: var(--addp-shadow-hover);
}

.card-content {
  text-align: center;
  padding: 20px;
}

.card-content h2 {
  margin: 15px 0 10px 0;
  font-size: 20px;
  color: var(--addp-text-primary);
}

.card-content p {
  color: var(--addp-text-tertiary);
  font-size: 14px;
  margin: 0;
}
</style>
