<template>
  <el-card shadow="never" class="resource-tree">
    <template #header>
      <div class="tree-header">
        <span>存储引擎</span>
        <el-button size="small" :loading="loading" @click="$emit('refresh')">
          <el-icon><Refresh /></el-icon>
        </el-button>
      </div>
    </template>

    <el-tree
      :data="treeData"
      :props="treeProps"
      node-key="id"
      :highlight-current="true"
      :expand-on-click-node="false"
      @node-click="handleNodeClick"
      v-loading="loading"
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
  </el-card>
</template>

<script setup>
import { Refresh, Folder, Collection, Document } from '@element-plus/icons-vue'

defineProps({
  treeData: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['refresh', 'node-click'])

const treeProps = {
  label: 'label',
  children: 'children'
}

const handleNodeClick = (nodeData) => {
  if (!nodeData || nodeData.type === 'resource') return
  emit('node-click', nodeData)
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
  overflow: auto;
}

.tree-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
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
