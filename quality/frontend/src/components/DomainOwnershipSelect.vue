<template>
  <div class="domain-ownership-select">
    <BusinessDomainSelect :options="domains" show-code
      :model-value="modelValue"
      clearable filterable
      :loading="loading"
      :disabled="!filter && (!canRead || loading || !!error)"
      :placeholder="t(filter ? 'quality.domain.all' : 'quality.domain.public')"
      @update:model-value="$emit('update:modelValue', $event === '' || $event == null ? null : $event)"
    >
      <el-option v-if="filter" :value="0" :label="t('quality.domain.public')" />
      <el-option v-if="modelValue && !domains.some(domain => domain.id === modelValue)"
        :value="modelValue" :label="t('quality.domain.unresolved', { id: modelValue })" />
    </BusinessDomainSelect>
    <el-alert v-if="!canRead || error" type="warning" :closable="false"
      :title="t(filter
        ? (canRead ? 'quality.domain.filterLoadFailed' : 'quality.domain.filterNoPermission')
        : (canRead ? 'quality.domain.loadFailed' : 'quality.domain.noPermission'))" />
    <el-button v-if="canRead && error" link @click="load">{{ t('quality.domain.retry') }}</el-button>
  </div>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { standardDomainAPI } from '../api/quality'
import { BusinessDomainSelect, buildBusinessDomainOptions } from '@common-ui'

const props = defineProps({ modelValue: { type: Number, default: null }, canRead: Boolean, filter: Boolean })
const emit = defineEmits(['update:modelValue', 'loaded'])
const { t } = useI18n()
const domains = ref([]), loading = ref(false), error = ref(false)
let sequence = 0
const load = async () => {
  const current = ++sequence
  domains.value = []
  emit('loaded', [])
  error.value = false
  loading.value = props.canRead
  if (!props.canRead) return
  try {
    const result = buildBusinessDomainOptions(await standardDomainAPI.list())
    if (current === sequence) {
      domains.value = result
      emit('loaded', result)
    }
  } catch {
    if (current === sequence) error.value = true
  } finally {
    if (current === sequence) loading.value = false
  }
}
watch(() => props.canRead, load, { immediate: true })
onBeforeUnmount(() => { sequence++ })
</script>

<style scoped>
.domain-ownership-select { width: 100%; }
</style>
