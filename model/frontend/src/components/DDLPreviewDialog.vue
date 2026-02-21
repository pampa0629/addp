<template>
  <el-dialog
    v-model="visible"
    title="DDL 预览"
    width="700px"
    :before-close="handleClose"
  >
    <div class="ddl-wrapper">
      <pre class="ddl-code">{{ ddl }}</pre>
    </div>
    <template #footer>
      <el-button @click="handleClose">关闭</el-button>
      <el-button type="primary" @click="handleCopy">
        <el-icon><CopyDocument /></el-icon>
        复制到剪贴板
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  },
  ddl: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const handleClose = () => {
  visible.value = false
}

const handleCopy = async () => {
  try {
    await navigator.clipboard.writeText(props.ddl)
    ElMessage.success('DDL 已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请手动选择文本复制')
  }
}
</script>

<style scoped>
.ddl-wrapper {
  background: var(--el-fill-color-darker, #1a1a2e);
  border-radius: 6px;
  padding: 16px;
  max-height: 400px;
  overflow-y: auto;
}

.ddl-code {
  margin: 0;
  font-family: 'JetBrains Mono', 'Fira Code', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-color-primary-light-3, #79c0ff);
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
