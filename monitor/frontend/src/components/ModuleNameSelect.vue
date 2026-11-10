<template>
  <div class="module-name-filter">
    <el-select :model-value="modelValue" :loading="loading" :placeholder="t('monitor.process.moduleFilter')" :aria-label="t('monitor.process.moduleFilter')" filterable clearable @update:model-value="emit('update:modelValue', $event || '')">
      <el-option v-for="name in names" :key="name" :label="name" :value="name" />
    </el-select>
    <small v-if="failed" role="status">{{ t('monitor.process.moduleListFailed') }}</small>
  </div>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import client from '../api/client'

const props = defineProps({
  modelValue: { type: String, default: '' },
  identity: { type: String, required: true },
  suspended: Boolean,
  revision: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const requests = createLatestRequestScope()
const names = ref([]), loading = ref(false), failed = ref(false)

watch([() => props.identity, () => props.suspended, () => props.revision], async ([identity, suspended], [previousIdentity] = []) => {
  const ticket = requests.invalidate()
  if (suspended || identity !== previousIdentity) names.value = []
  failed.value = false
  loading.value = !suspended
  if (suspended) return

  const result = await requests.request(config => client.get('/system/platform/modules', config)).catch(() => null)
  if (!requests.current(ticket)) return
  loading.value = false
  failed.value = !Array.isArray(result?.modules)
  names.value = failed.value ? [] : [...new Set(result.modules.map(module => module?.module_name).filter(name => typeof name === 'string' && name))].sort()
}, { immediate: true })
onBeforeUnmount(() => requests.dispose())
</script>

<style scoped>
.module-name-filter { width: 200px; max-width: 100%; }
.module-name-filter .el-select { width: 100%; }
small { display: block; margin-top: 4px; color: var(--el-color-warning); }
</style>
