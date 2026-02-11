<template>
  <el-dialog
    :model-value="visible"
    width="640px"
    title="选择对象存储目录"
    @close="handleClose"
  >
    <div class="picker-container">
      <div class="picker-header">
        <div class="bucket-info">存储桶：{{ bucketName || '未知' }}</div>
        <el-breadcrumb class="picker-breadcrumb" separator="/">
          <el-breadcrumb-item>
            <el-link type="primary" @click="handleBreadcrumbClick('')">
              {{ bucketName || '根目录' }}
            </el-link>
          </el-breadcrumb-item>
          <el-breadcrumb-item
            v-for="item in breadcrumbItems"
            :key="item.path"
          >
            <el-link type="primary" @click="handleBreadcrumbClick(item.path)">
              {{ item.label || '未命名' }}
            </el-link>
          </el-breadcrumb-item>
        </el-breadcrumb>
      </div>

      <div class="current-prefix">
        当前目录：{{ displayPath(currentPrefix) }}
      </div>

      <el-skeleton v-if="loading" :rows="4" animated />

      <el-empty
        v-else-if="directories.length === 0"
        description="暂无子目录"
      />

      <el-table
        v-else
        :data="directories"
        height="320"
        border
        highlight-current-row
        @row-click="handleRowClick"
        :current-row-key="selectedPath"
        row-key="path"
      >
        <el-table-column label="目录" min-width="300">
          <template #default="{ row }">
            <span class="directory-cell">
              <el-icon :size="16" class="directory-icon"><Folder /></el-icon>
              <span class="directory-name">{{ row.name }}</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" align="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click.stop="enterDirectory(row)">
              打开
            </el-button>
            <el-button link size="small" @click.stop="selectDirectory(row.path)">
              选择
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <template #footer>
      <div class="picker-footer">
        <div class="selected-info">
          已选择：{{ displayPath(selectedPath || currentPrefix) }}
        </div>
        <div class="footer-actions">
          <el-button @click="handleClose">取消</el-button>
          <el-button
            type="primary"
            :disabled="!canConfirm"
            @click="selectDirectory(selectedPath || currentPrefix)"
          >
            使用当前目录
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Folder } from '@element-plus/icons-vue'

import { objectStorageAPI } from '@/api/objectStorage'

const props = defineProps({
  visible: { type: Boolean, default: false },
  scope: { type: String, default: '' },
  resourceId: { type: Number, default: null },
  initialPrefix: { type: String, default: '' }
})

const emit = defineEmits(['update:visible', 'selected'])

const loading = ref(false)
const bucketName = ref('')
const currentPrefix = ref('')
const directories = ref([])
const selectedPath = ref('')

const canConfirm = computed(() => !!(props.resourceId && props.scope))

const breadcrumbItems = computed(() => {
  const prefix = currentPrefix.value
  if (!prefix) {
    return []
  }
  const segments = prefix.split('/').filter(Boolean)
  const items = []
  let accumulated = ''
  segments.forEach(segment => {
    accumulated += segment + '/'
    items.push({
      label: segment,
      path: accumulated
    })
  })
  return items
})

const normalizeDirectory = (value) => {
  if (!value) return ''
  let normalized = value.trim()
  normalized = normalized.replace(/^\/+/, '')
  if (normalized && !normalized.endsWith('/')) {
    normalized += '/'
  }
  return normalized
}

const displayPath = (value) => {
  const normalized = normalizeDirectory(value)
  return normalized ? `/${normalized}` : '/'
}

const loadDirectories = async (prefix = '') => {
  if (!props.resourceId || !props.scope) {
    return
  }
  loading.value = true
  try {
    const payload = {
      scope: props.scope,
      engine_id: props.resourceId,
      prefix: normalizeDirectory(prefix)
    }
    const result = await objectStorageAPI.browse(payload)
    bucketName.value = result?.bucket || ''
    currentPrefix.value = result?.prefix || ''
    directories.value = Array.isArray(result?.directories) ? result.directories : []
    if (
      selectedPath.value &&
      selectedPath.value !== (result?.prefix || '') &&
      !directories.value.some(dir => dir.path === selectedPath.value)
    ) {
      selectedPath.value = ''
    }
  } catch (error) {
    console.error('浏览对象存储目录失败:', error)
    ElMessage.error(error.response?.data?.error || '加载目录失败')
  } finally {
    loading.value = false
  }
}

const openWithPrefix = (prefix) => {
  loadDirectories(prefix)
}

const enterDirectory = (row) => {
  if (row?.path) {
    openWithPrefix(row.path)
  }
}

const handleRowClick = (row) => {
  if (row?.path) {
    selectedPath.value = row.path
  }
}

const selectDirectory = (path) => {
  if (!canConfirm.value) {
    ElMessage.warning('请先选择对象存储资源')
    return
  }
  const normalized = normalizeDirectory(path)
  emit('selected', normalized)
  emit('update:visible', false)
}

const handleBreadcrumbClick = (path) => {
  if (loading.value) return
  openWithPrefix(path)
}

const handleClose = () => {
  emit('update:visible', false)
}

watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      const initialDir = normalizeDirectory(props.initialPrefix)
      selectedPath.value = initialDir
      loadDirectories(initialDir)
    } else {
      directories.value = []
      currentPrefix.value = ''
      selectedPath.value = ''
      loading.value = false
    }
  }
)
</script>

<style scoped>
.picker-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.picker-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.bucket-info {
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.picker-breadcrumb {
  font-size: 13px;
}

.current-prefix {
  font-size: 13px;
  color: #409eff;
}

.directory-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.directory-icon {
  color: #e6a23c;
}

.directory-name {
  color: var(--addp-text-primary);
}

.picker-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.selected-info {
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.footer-actions {
  display: flex;
  gap: 12px;
}
</style>
