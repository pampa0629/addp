<template>
  <el-form-item :label="t('graph.knowledgeGraph.engine')" prop="engine_id">
    <el-select :model-value="modelValue.engine_id" :loading="loadingEngines" :placeholder="t('graph.knowledgeGraph.selectEngine')" filterable style="width:100%" @update:model-value="selectEngine" @visible-change="visible => visible && loadEngines()">
      <el-option v-for="engine in engines" :key="engine.id" :label="`${engine.name} · ${engine.engine_type} · ${t(`common.engineStatus.${engineSelectionState(engine)}`)}`" :value="engine.id" :disabled="!isEngineSelectable(engine)" />
    </el-select>
  </el-form-item>
  <el-form-item :label="t('graph.knowledgeGraph.targetGraph')" prop="catalog_path">
    <el-select :model-value="selectedPath" :loading="loadingTargets" :disabled="!available" :placeholder="t('graph.knowledgeGraph.selectGraph')" filterable style="width:100%" @update:model-value="selectPath">
      <el-option v-for="target in targets" :key="pathKey(target.path)" :label="target.path.segments.slice(1).map(segment => segment.name).join(' / ')" :value="pathKey(target.path)" />
    </el-select>
  </el-form-item>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { engineSelectionState, isEngineSelectable } from '@common-ui'
import { engineAPI } from '../api/ontology'
const props = defineProps({ modelValue: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const engines = ref([]), targets = ref([]), loadingEngines = ref(false), loadingTargets = ref(false)
let targetGeneration = 0, engineGeneration = 0
const pathKey = path => path ? JSON.stringify(path) : ''
const selectedPath = computed(() => pathKey(props.modelValue.catalog_path))
const available = computed(() => isEngineSelectable(engines.value.find(engine => engine.id === props.modelValue.engine_id)))
async function loadEngines() {
  const generation = ++engineGeneration
  loadingEngines.value = true
  try {
    const result = await engineAPI.getGraphEngines()
    if (generation === engineGeneration) {
      engines.value = result
      if (props.modelValue.engine_id && !available.value) { ++targetGeneration; targets.value = []; emit('update:modelValue', { ...props.modelValue, catalog_path: null }) }
    }
  }
  catch { if (generation === engineGeneration) ElMessage.error(t('graph.knowledgeGraph.loadEnginesFailed')) }
  finally { if (generation === engineGeneration) loadingEngines.value = false }
}
async function selectEngine(engineId) {
  emit('update:modelValue', { ...props.modelValue, engine_id: engineId, catalog_path: null })
  targets.value = []
  const generation = ++targetGeneration
  if (!isEngineSelectable(engines.value.find(engine => engine.id === engineId))) return
  loadingTargets.value = true
  try {
    const result = await engineAPI.getGraphTargets(engineId)
    if (generation === targetGeneration) targets.value = result
  } catch { if (generation === targetGeneration) ElMessage.error(t('graph.knowledgeGraph.loadGraphsFailed')) }
  finally { if (generation === targetGeneration) loadingTargets.value = false }
}
function selectPath(key) {
  const target = targets.value.find(target => pathKey(target.path) === key)
  if (available.value && target) emit('update:modelValue', { ...props.modelValue, catalog_path: target.path })
}
onMounted(async () => {
  await loadEngines()
  if (props.modelValue.engine_id && available.value) {
    const generation = ++targetGeneration
    try { const result = await engineAPI.getGraphTargets(props.modelValue.engine_id); if (generation === targetGeneration) targets.value = result }
    catch { if (generation === targetGeneration) ElMessage.error(t('graph.knowledgeGraph.loadGraphsFailed')) }
  }
})
onBeforeUnmount(() => { targetGeneration++; engineGeneration++ })
</script>
