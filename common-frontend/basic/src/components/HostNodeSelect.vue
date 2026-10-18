<template>
  <div class="host-node-select">
    <el-select ref="input" :model-value="modelValue" filterable remote :remote-method="searchNodes" :loading="loading" :disabled="!allowed || disabled" :placeholder="placeholder" :aria-label="label" data-testid="target-node" @update:model-value="select">
      <el-option v-for="host in options" :key="host.node_id" :value="host.node_id" :label="host.display_name" />
    </el-select>
    <el-button v-if="allowed && rows.length < total" text :loading="loading" @click="load(search, page + 1)">{{ moreLabel }}</el-button>
    <el-alert v-if="error" :title="errorMessage" type="error" :closable="false" />
  </div>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
const isTargetUUID = value => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(value) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/u.test(value)
const props = defineProps({ modelValue: { type: String, default: '' }, selected: { type: Object, default: null }, disabled: Boolean, allowed: Boolean, identity: { type: String, required: true }, api: { type: Object, required: true }, label: String, placeholder: String, moreLabel: String, errorMessage: String })
const emit = defineEmits(['update:modelValue', 'selected'])
const allowed = computed(() => props.allowed)
const rows = ref([]), total = ref(0), loading = ref(false), error = ref(''), input = ref(null)
const options = computed(() => !allowed.value ? [] : props.selected && !rows.value.some(host => host.node_id === props.selected.node_id) ? [props.selected, ...rows.value] : rows.value)
let page = 1, search = '', ticket = 0, controller, selectionController, selectionTicket = 0
function invalidate() { selectionTicket++; selectionController?.abort(); ticket++; controller?.abort(); rows.value = []; total.value = 0; error.value = ''; loading.value = false }
async function load(term = '', nextPage = 1) {
  controller?.abort(); const own = ++ticket; controller = new AbortController()
  if (!allowed.value) return
  if (nextPage === 1) rows.value = []
  loading.value = true; error.value = ''
  try {
    const result = await props.api.nodes({ page: nextPage, page_size: 20, ...(term.trim() ? { search: term.trim() } : {}) }, { signal: controller.signal })
    if (own !== ticket) return
    if (!Array.isArray(result.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.data.some(host => !isTargetUUID(host.node_id) || typeof host.display_name !== 'string' || !host.display_name.trim() || !Array.isArray(host.addresses) || host.addresses.some(address => typeof address !== 'string'))) throw new Error('invalid_hosts')
    rows.value = nextPage === 1 ? result.data : [...rows.value, ...result.data.filter(host => !rows.value.some(existing => existing.node_id === host.node_id))]
    total.value = result.total; page = nextPage; search = term
  } catch { if (own === ticket) error.value = 'host_lookup_failed' }
  finally { if (own === ticket) loading.value = false }
}
function searchNodes(value) { return load(value, 1) }
function select(value) { const host = options.value.find(item => item.node_id === value); emit('update:modelValue', host?.node_id || ''); emit('selected', host || null) }
watch(() => JSON.stringify([props.identity, props.allowed]), () => { invalidate(); load() }, { immediate: true })
watch(() => props.modelValue, async value => {
  selectionController?.abort(); const own = ++selectionTicket
  if (!value || props.selected?.node_id === value || !allowed.value) return
  const cached = rows.value.find(host => host.node_id === value)
  if (cached) { emit('selected', cached); return }
  if (!isTargetUUID(value)) { emit('selected', null); error.value = 'host_lookup_failed'; return }
  selectionController = new AbortController()
  try {
    const host = await props.api.node(value, { signal: selectionController.signal })
    if (own !== selectionTicket) return
    if (host?.node_id !== value || typeof host.display_name !== 'string' || !host.display_name.trim() || !Array.isArray(host.addresses) || host.addresses.some(address => typeof address !== 'string')) throw new Error('invalid_host')
    emit('selected', host)
  } catch { if (own === selectionTicket) { emit('selected', null); error.value = 'host_lookup_failed' } }
}, { immediate: true })
onUnmounted(invalidate)
defineExpose({ focus: () => input.value?.focus() })
</script>
<style scoped>
.host-node-select { width: 100%; min-width: 0; }
.host-node-select :deep(.el-select) { width: 100%; }
</style>
