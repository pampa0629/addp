<template>
  <el-dialog
    v-model="visible"
    :title="$t('standard.ddlPreview.title')"
    width="700px"
    :before-close="handleClose"
  >
    <div class="ddl-wrapper">
      <pre class="ddl-code">{{ ddl }}</pre>
    </div>
    <template #footer>
      <el-button @click="handleClose">{{ $t('standard.ddlPreview.close') }}</el-button>
      <el-button type="primary" @click="handleCopy">
        <el-icon><CopyDocument /></el-icon>
        {{ $t('standard.ddlPreview.copy') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
    ElMessage.success(t('standard.ddlPreview.copySuccess'))
  } catch {
    ElMessage.error(t('standard.ddlPreview.copyFailed'))
  }
}
</script>

<style scoped>
.ddl-wrapper {
  background: var(--el-fill-color-darker);
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
  color: var(--el-color-primary-light-3);
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
