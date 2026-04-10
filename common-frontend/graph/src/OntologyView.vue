<template>
  <div ref="containerRef" class="ontology-view">
    <div v-if="isEmpty" class="empty-hint">
      <el-empty :description="t('graph.noEntities')" :image-size="60" />
    </div>
    <!-- 右侧属性面板 -->
    <div v-if="selectedNode" class="node-detail">
      <div class="node-detail-header">
        <span :style="{ color: selectedNode.color || '#6366f1' }">● </span>
        <strong>{{ selectedNode.label }}</strong>
        <el-button text size="small" style="margin-left:auto" @click="selectedNode = null">×</el-button>
      </div>
      <div class="node-detail-body">
        <div class="detail-row"><span class="detail-key">{{ t('graph.identifier') }}</span><span>{{ selectedNode.name }}</span></div>
        <div class="detail-row"><span class="detail-key">{{ t('graph.displayName') }}</span><span>{{ selectedNode.label }}</span></div>
        <div v-if="selectedNode._props && selectedNode._props.length" class="detail-section">
          <div class="detail-section-title">{{ t('graph.propList') }}</div>
          <div v-for="p in selectedNode._props" :key="p.name" class="detail-row">
            <span class="detail-key">{{ p.label || p.name }}</span>
            <span class="detail-val">{{ p.data_type }}{{ p.required ? ' *' : '' }}{{ p.unique ? ` ${t('graph.unique')}` : '' }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import G6 from '@antv/g6'

const { t } = useI18n()

const props = defineProps({
  entityTypes: { type: Array, default: () => [] },
  relationTypes: { type: Array, default: () => [] },
  readonly: { type: Boolean, default: false }
})

const emit = defineEmits(['node-click', 'edge-click', 'canvas-click', 'edge-create'])

const containerRef = ref(null)
const selectedNode = ref(null)
let graphInstance = null

const isEmpty = computed(() => props.entityTypes.length === 0)

function getThemeColor(varName, fallback) {
  if (containerRef.value) {
    const val = getComputedStyle(containerRef.value).getPropertyValue(varName).trim()
    if (val) return val
  }
  return fallback
}

function buildG6Data() {
  const edgeLabelColor = getThemeColor('--addp-text-secondary', '#909399')
  const inheritColor = getThemeColor('--addp-text-tertiary', '#a3a6ad')

  const nodes = props.entityTypes.map(et => ({
    id: String(et.id),
    label: (et.label || et.name).substring(0, 16),
    _meta: et,
    style: {
      fill: et.color || '#6366f1',
      stroke: '#fff',
      lineWidth: 2,
      r: 30
    },
    labelCfg: { style: { fill: '#fff', fontSize: 12, fontWeight: 'bold' } }
  }))

  const edges = [
    // 关系类型：实线有向边
    ...props.relationTypes
      .filter(rt => rt.source_type_id && rt.target_type_id)
      .map(rt => ({
        id: `rel_${rt.id}`,
        source: String(rt.source_type_id),
        target: String(rt.target_type_id),
        label: rt.label || rt.name,
        style: { stroke: rt.color || '#8B8B8B', lineWidth: 1.5, endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: rt.color || '#8B8B8B' } },
        labelCfg: { style: { fill: edgeLabelColor, fontSize: 11 }, autoRotate: true }
      })),
    // 继承关系：虚线边
    ...props.entityTypes
      .filter(et => et.parent_id)
      .map(et => ({
        id: `inherit_${et.id}`,
        source: String(et.parent_id),
        target: String(et.id),
        label: '继承',
        style: { lineDash: [4, 4], stroke: inheritColor, lineWidth: 1.5, endArrow: { path: G6.Arrow.triangle(6, 4, 0), fill: inheritColor } },
        labelCfg: { style: { fill: inheritColor, fontSize: 10 }, autoRotate: true }
      }))
  ]

  return { nodes, edges }
}

function initGraph() {
  if (!containerRef.value || isEmpty.value || graphInstance) return

  const width = containerRef.value.offsetWidth || 800
  const height = containerRef.value.offsetHeight || 500

  graphInstance = new G6.Graph({
    container: containerRef.value,
    width,
    height,
    fitView: true,
    fitViewPadding: 40,
    modes: {
      default: props.readonly
        ? ['drag-canvas', 'zoom-canvas']
        : ['drag-canvas', 'zoom-canvas', 'drag-node']
    },
    layout: {
      type: 'dagre',
      rankdir: 'LR',
      nodesep: 60,
      ranksep: 100
    },
    defaultNode: { type: 'circle', size: 60 },
    defaultEdge: { type: 'quadratic' }
  })

  graphInstance.on('node:click', evt => {
    const model = evt.item.getModel()
    const meta = model._meta
    selectedNode.value = {
      ...meta,
      _props: Array.isArray(meta.properties) ? meta.properties : []
    }
    emit('node-click', meta)
  })

  graphInstance.on('edge:click', evt => {
    emit('edge-click', evt.item.getModel())
  })

  graphInstance.on('canvas:click', () => {
    selectedNode.value = null
    emit('canvas-click')
  })

  graphInstance.data(buildG6Data())
  graphInstance.render()
}

function rerender() {
  if (graphInstance) {
    graphInstance.destroy()
    graphInstance = null
  }
  selectedNode.value = null
  nextTick(() => {
    if (!isEmpty.value) initGraph()
  })
}

let resizeObserver = null

onMounted(async () => {
  await nextTick()
  if (!isEmpty.value) initGraph()
  resizeObserver = new ResizeObserver(() => {
    if (graphInstance && containerRef.value) {
      const w = containerRef.value.offsetWidth
      const h = containerRef.value.offsetHeight
      if (w > 50 && h > 50) {
        graphInstance.changeSize(w, h)
        try { graphInstance.fitView(40) } catch (_) {}
      }
    }
  })
  if (containerRef.value) resizeObserver.observe(containerRef.value)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  graphInstance?.destroy()
  graphInstance = null
})

watch(() => [props.entityTypes, props.relationTypes], rerender, { deep: true })
</script>

<style scoped>
.ontology-view {
  width: 100%;
  height: 100%;
  min-height: 400px;
  position: relative;
  background: var(--addp-bg-primary, #fafbfc);
  border-radius: 4px;
  overflow: hidden;
}

.empty-hint {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}

.node-detail {
  position: absolute;
  top: 12px;
  right: 12px;
  width: 220px;
  background: var(--addp-bg-primary, #fff);
  border: 1px solid var(--addp-border-color, #dcdfe6);
  border-radius: 6px;
  padding: 10px 14px;
  box-shadow: var(--addp-shadow-card, 0 2px 8px rgba(0,0,0,0.1));
  font-size: 12px;
  max-height: 60%;
  overflow-y: auto;
  z-index: 10;
}

.node-detail-header {
  display: flex;
  align-items: center;
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--addp-text-primary, #303133);
}

.detail-row {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  border-bottom: 1px solid var(--addp-border-color-light, #f0f0f0);
}

.detail-key {
  color: var(--addp-text-tertiary, #909399);
  width: 60px;
  flex-shrink: 0;
}

.detail-val {
  color: var(--addp-text-primary, #303133);
}

.detail-section {
  margin-top: 8px;
}

.detail-section-title {
  font-weight: 600;
  color: var(--addp-text-secondary, #606266);
  margin-bottom: 4px;
}
</style>
