<template>
  <el-card class="asset-card" shadow="hover" @click="$router.push(`/portal/assets/${asset.id}`)">
    <div class="card-header">
      <el-tag size="small" class="type-tag">{{ asset.type_name || '未知类型' }}</el-tag>
    </div>
    <div class="asset-name">{{ asset.name }}</div>
    <div class="catalog-path" v-if="asset.catalog_name">
      <el-icon><FolderOpened /></el-icon>
      <span>{{ asset.catalog_name }}</span>
    </div>
    <p class="description">{{ asset.description || '暂无描述' }}</p>
    <div class="tags" v-if="asset.tags?.length">
      <el-tag
        v-for="tag in asset.tags.slice(0, 3)"
        :key="tag"
        size="small"
        type="info"
        class="tag-item"
      >{{ tag }}</el-tag>
      <span v-if="asset.tags.length > 3" class="more-tags">+{{ asset.tags.length - 3 }}</span>
    </div>
    <div class="card-footer">
      <span class="date">{{ formatDate(asset.updated_at) }}</span>
      <span class="owner" v-if="asset.owner_name">{{ asset.owner_name }}</span>
    </div>
  </el-card>
</template>

<script setup>
import { formatDate } from '@common-ui'
import { FolderOpened } from '@element-plus/icons-vue'

defineProps({
  asset: {
    type: Object,
    required: true
  }
})
</script>

<style scoped>
.asset-card {
  cursor: pointer;
  height: 100%;
  transition: transform 0.15s;
}

.asset-card:hover {
  transform: translateY(-2px);
}

.card-header {
  margin-bottom: 8px;
}

.type-tag {
  font-size: 11px;
}

.asset-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.catalog-path {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}

.description {
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin: 0 0 10px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  line-height: 1.5;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 10px;
}

.tag-item {
  font-size: 11px;
}

.more-tags {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  line-height: 20px;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 8px;
  margin-top: auto;
}
</style>
