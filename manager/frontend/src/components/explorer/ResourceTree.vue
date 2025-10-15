<template>
  <el-card shadow="never" class="resource-tree">
    <template #header>
      <div class="tree-header">
        <div class="header-info">
          <span class="header-title">存储引擎</span>
          <span class="resource-count" v-if="resources.length">
            共 {{ resources.length }} 个
          </span>
        </div>
        <div class="header-actions">
          <el-button
            size="small"
            :loading="loading || loadingResources"
            :disabled="loadingResources"
            @click="emitRefresh"
          >
            <el-icon><Refresh /></el-icon>
          </el-button>
        </div>
      </div>
    </template>

    <div class="tree-container" v-loading="loading">
      <el-empty
        v-if="!resources.length && !loadingResources"
        description="暂无存储引擎"
        class="empty-placeholder"
      />
      <el-empty
        v-else-if="!loading && !hasTreeData"
        description="无可用元数据，请先执行扫描"
        class="empty-placeholder"
      />
      <el-tree
        v-else
        :key="treeKey"
        :data="treeData"
        :props="treeProps"
        node-key="id"
        :highlight-current="true"
        :expand-on-click-node="false"
        :default-expanded-keys="expandedKeys"
        @node-click="handleNodeClick"
      >
        <template #default="{ data }">
          <span class="tree-node" :class="data.type">
            <el-icon v-if="data.type === 'resource'"><Collection /></el-icon>
            <el-icon v-else-if="['schema', 'bucket', 'directory'].includes(data.type)"><Folder /></el-icon>
            <el-icon v-else><Document /></el-icon>
            <span class="label" :title="data.label">{{ data.label }}</span>
          </span>
        </template>
      </el-tree>
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue'
import { Refresh, Folder, Collection, Document } from '@element-plus/icons-vue'

const props = defineProps({
  resources: {
    type: Array,
    default: () => []
  },
  treeData: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  loadingResources: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['refresh', 'node-click'])

const hasTreeData = computed(() => {
  if (!Array.isArray(props.treeData)) return false
  return props.treeData.some((node) => Array.isArray(node?.children) && node.children.length > 0)
})

const treeProps = {
  label: 'label',
  children: 'children'
}

const expandedKeys = computed(() => props.treeData.map((item) => item.id))
const treeKey = computed(() => expandedKeys.value.join('|') || 'resource-tree')

const handleNodeClick = (nodeData) => {
  if (!nodeData || nodeData.type === 'resource') return
  emit('node-click', nodeData)
}

const emitRefresh = () => {
  emit('refresh')
}
</script>

<style scoped>
.resource-tree {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.resource-tree :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 0;
}

.tree-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.header-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-title {
  font-weight: 500;
}

.resource-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tree-container {
  flex: 1;
  overflow: auto;
  padding: 0 16px 16px;
}

.empty-placeholder {
  margin-top: 60px;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tree-node .label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
