<template>
  <section v-if="dataset || value" class="key-value-preview" data-testid="key-value-preview">
    <div v-if="dataset" class="dataset-toolbar">
      <span>{{ t('keyValuePreview.database') }}: DB {{ dataset.database }}</span>
      <el-input v-model="prefixInput" class="prefix-input" size="small" :disabled="busy" :aria-label="t('keyValuePreview.prefix')" :placeholder="t('keyValuePreview.prefixExample')" clearable @keyup.enter="applyPrefix" @clear="applyPrefix" />
      <el-button size="small" :disabled="busy" @click="applyPrefix">{{ t('keyValuePreview.filter') }}</el-button>
      <el-button size="small" :disabled="busy" @click="browse('')">{{ t('keyValuePreview.refresh') }}</el-button>
      <el-button size="small" :disabled="busy || dataset.complete" @click="browse(dataset.next_cursor)">{{ t('keyValuePreview.nextBatch') }}</el-button>
    </div>
    <el-alert v-if="failed" :title="t('keyValuePreview.failed')" type="error" :closable="false" />
    <el-table v-if="dataset" :data="dataset.keys" :empty-text="t(dataset.complete ? 'keyValuePreview.emptyBatchComplete' : 'keyValuePreview.emptyBatchContinue')" v-loading="busy" row-key="key" size="small" border data-testid="keyspace-keys">
      <el-table-column :label="t('keyValuePreview.key')" min-width="240">
        <template #default="{ row }"><el-button link type="primary" @click="select(row)">{{ keyLabel(row.name) }}</el-button></template>
      </el-table-column>
      <el-table-column prop="facts.native_type" :label="t('keyValuePreview.nativeType')" width="100" />
      <el-table-column :label="t('keyValuePreview.value')" min-width="280" show-overflow-tooltip>
        <template #default="{ row }"><span class="value-sample" data-testid="key-value-sample">{{ sampleLabel(row) }}<span v-if="row.truncated" :title="t('keyValuePreview.truncated')"> …</span></span></template>
      </el-table-column>
      <el-table-column :label="t('keyValuePreview.summary')" min-width="120"><template #default="{ row }">{{ t(row.facts.native_type === 'string' ? 'keyValuePreview.bytes' : 'keyValuePreview.elements', { count: row.facts.length }) }}</template></el-table-column>
      <el-table-column :label="t('keyValuePreview.ttl')" min-width="120"><template #default="{ row }">{{ ttlLabel(row.facts.ttl_millis) }}</template></el-table-column>
    </el-table>
    <template v-if="value">
    <div v-if="selectedName" class="native-value" data-testid="selected-key-name">{{ selectedName }}</div>
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
    </template>
  </section>
</template>

<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps({ data: { type: Object, required: true }, loadKeyValue: { type: Function, default: null } })
const { t } = useI18n()
const dataset = ref(null)
const value = ref(null)
const selectedName = ref('')
const busy = ref(false)
const failed = ref(false)
const prefixInput = ref('')
const appliedPrefix = ref('')
let sequence = 0
watch(() => props.data, data => {
 sequence++
 dataset.value = data?.keyspace || null
 value.value = data?.key_value || null
 selectedName.value = ''
 busy.value = false
 failed.value = false
 prefixInput.value = ''
 appliedPrefix.value = ''
}, { immediate: true })
onBeforeUnmount(() => { sequence++ })
const ttlLabel = millis => millis === -1 ? t('keyValuePreview.persistent') : `${millis} ms`
const ttl = computed(() => ttlLabel(value.value?.facts.ttl_millis))
const keyLabel = bytes => bytes?.encoding === 'base64' ? `Base64: ${bytes.value}` : JSON.stringify(bytes?.value || '')
const display = bytes => bytes ? `${bytes.encoding === 'base64' ? 'Base64' : 'UTF-8'}: ${bytes.value}` : ''
function sampleLabel(sample) {
 if (sample.value) return keyLabel(sample.value)
 const entries = sample.entries.map(entry => {
  if (entry.field) return `${keyLabel(entry.field)}: ${keyLabel(entry.value)}`
  if (entry.id) return `${entry.id}: [${entry.fields.map(field => `[${keyLabel(field.name)}, ${keyLabel(field.value)}]`).join(', ')}]`
  if (entry.score !== undefined) return `${keyLabel(entry.value)} (${entry.score})`
  return keyLabel(entry.value)
 })
 return sample.facts.native_type === 'hash' ? `{${entries.join(', ')}}` : `[${entries.join(', ')}]`
}
async function request(options) {
 if (!props.loadKeyValue || busy.value) return null
 const current = ++sequence
 busy.value = true
 failed.value = false
 try {
  const data = await props.loadKeyValue(options)
  return current === sequence ? data : null
 } catch {
  if (current === sequence) failed.value = true
  return null
 } finally {
  if (current === sequence) busy.value = false
 }
}
async function browse(cursor, prefix = appliedPrefix.value) {
 const data = await request({ key_cursor: cursor, key_prefix: prefix })
 if (data?.keyspace) {
  dataset.value = data.keyspace
  appliedPrefix.value = prefix
  value.value = null
  selectedName.value = ''
 }
}
function applyPrefix() {
 return browse('', prefixInput.value)
}
async function select(row) {
 const data = await request({ key_name: row.key })
 if (data?.key_value) { value.value = data.key_value; selectedName.value = keyLabel(row.name) }
}
</script>

<style scoped>
.key-value-preview { padding: 12px; display: flex; flex-direction: column; gap: 12px; }
.dataset-toolbar { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.prefix-input { flex: 1 1 180px; max-width: 320px; }
.native-value { white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--el-font-family-monospace, monospace); }
.value-sample { white-space: nowrap; font-family: var(--el-font-family-monospace, monospace); }
.stream-fields { margin: 0; padding-inline-start: 20px; }
</style>
