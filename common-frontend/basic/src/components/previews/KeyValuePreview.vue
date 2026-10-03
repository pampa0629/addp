<template>
  <section v-if="value" class="key-value-preview" data-testid="key-value-preview">
    <el-descriptions :column="3" border>
      <el-descriptions-item :label="t('keyValuePreview.nativeType')">{{ value.facts.native_type }}</el-descriptions-item>
      <el-descriptions-item :label="t('keyValuePreview.length')">{{ value.facts.length }}</el-descriptions-item>
      <el-descriptions-item :label="t('keyValuePreview.ttl')">{{ ttl }}</el-descriptions-item>
    </el-descriptions>
    <el-alert v-if="value.truncated" :title="t('keyValuePreview.truncated')" type="info" :closable="false" show-icon />
    <template v-if="value.value">
      <el-tag>{{ value.value.encoding === 'base64' ? 'Base64' : 'UTF-8' }}</el-tag>
      <pre class="native-value" data-testid="key-value-string">{{ value.value.value }}</pre>
    </template>
    <el-table v-else :data="value.entries" size="small" border>
      <el-table-column v-if="value.facts.native_type === 'list'" prop="index" :label="t('keyValuePreview.index')" width="100" />
      <el-table-column v-if="value.facts.native_type === 'hash'" :label="t('keyValuePreview.field')">
        <template #default="{ row }"><span class="native-value">{{ display(row.field) }}</span></template>
      </el-table-column>
      <el-table-column v-if="value.facts.native_type !== 'stream'" :label="t('keyValuePreview.value')">
        <template #default="{ row }"><span class="native-value">{{ display(row.value) }}</span></template>
      </el-table-column>
      <el-table-column v-if="value.facts.native_type === 'zset'" prop="score" :label="t('keyValuePreview.score')" />
      <el-table-column v-if="value.facts.native_type === 'stream'" prop="id" label="ID" />
      <el-table-column v-if="value.facts.native_type === 'stream'" :label="t('keyValuePreview.fields')">
        <template #default="{ row }">
          <ol class="stream-fields"><li v-for="(field, index) in row.fields" :key="index"><span class="native-value">{{ display(field.name) }} : {{ display(field.value) }}</span></li></ol>
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps({ data: { type: Object, required: true } })
const { t } = useI18n()
const value = computed(() => props.data?.key_value)
const ttl = computed(() => value.value?.facts.ttl_millis === -1 ? t('keyValuePreview.persistent') : `${value.value?.facts.ttl_millis} ms`)
const display = (bytes) => bytes ? `${bytes.encoding === 'base64' ? 'Base64' : 'UTF-8'}: ${bytes.value}` : ''
</script>

<style scoped>
.key-value-preview { padding: 12px; display: flex; flex-direction: column; gap: 12px; }
.native-value { white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--el-font-family-monospace, monospace); }
.stream-fields { margin: 0; padding-inline-start: 20px; }
</style>
