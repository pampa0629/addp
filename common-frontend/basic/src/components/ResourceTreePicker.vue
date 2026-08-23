<template>
  <div class="resource-tree-picker">
    <el-form-item v-if="showEngineSelector" :label="engineLabel">
      <el-select
        ref="engineSelectRef"
        v-model="selectedEngineValue"
        :placeholder="enginePlaceholder"
        :loading="loadingEngines"
        filterable
        :multiple="engineMultiple"
        collapse-tags
        collapse-tags-tooltip
        style="width: 100%"
        @change="handleEngineChange"
        @visible-change="handleEngineDropdownVisible"
      >
        <el-option
          v-for="engine in engines"
          :key="engine.id"
          :label="engineOptionLabel(engine)"
          :value="engine.id"
          :disabled="!isEngineSelectable(engine)"
        />
      </el-select>
    </el-form-item>

    <el-input
      v-if="showSearch"
      ref="searchInputRef"
      v-model="searchKeyword"
      class="picker-search"
      :placeholder="activeSearchPlaceholder"
      clearable
      :prefix-icon="Search"
    />

    <div class="tree-panel" :style="{ height: treeHeight }">
      <div v-if="isSearchMode" class="search-results" v-loading="searching">
        <div v-if="searchResults.length > 0" class="search-result-list">
          <button
            v-for="result in searchResults"
            :key="result.node.locator || result.node.id"
            type="button"
            class="search-result"
            :class="{ 'is-disabled': !isSelectableNode(result.node, result.engine) }"
            @click="handleSearchResultClick(result)"
          >
            <span class="search-result-main">
              <span class="search-result-label">{{ result.node.label }}</span>
              <span class="search-result-path">{{ searchResultPath(result.node, result.engine) }}</span>
            </span>
            <span class="search-result-engine">{{ result.engine?.name || result.engineName }}</span>
          </button>
        </div>
        <el-empty v-else-if="!searching" :description="searchEmptyText" />
      </div>

      <ResourceTree
        v-else
        ref="treeRef"
        :tree-data="treeData"
        :loading="loadingTree"
        :show-refresh-button="false"
        :current-node-key="currentNodeKey"
        v-model:expanded-keys="expandedKeys"
        :default-expand-root="true"
        :default-expand-all="false"
        :show-count="showCount"
        :lazy="true"
        :load="loadNode"
        :title="title"
        :height="treeHeight"
        card-shadow="never"
        @node-click="handleNodeClick"
        @node-expand="handleNodeExpand"
        @node-dblclick="handleNodeDblclick"
      >
        <template #node="{ data }">
          <span
            class="picker-node"
            :class="{ 'is-disabled': !isSelectableTreeNode(data) }"
            @dblclick.stop="handleNodeDblclick(data)"
          >
            <span class="picker-node-label">{{ data.label }}</span>
            <el-tag v-if="showDisabledLabel && disabledLabel && !isSelectableTreeNode(data)" size="small" type="info">
              {{ disabledLabel }}
            </el-tag>
          </span>
        </template>
      </ResourceTree>
    </div>

    <el-alert
      v-if="showSelectionSummary && currentSelection"
      class="selection-summary"
      type="success"
      :closable="false"
    >
      <template #title>
        {{ currentSelection.display.label }}
        <span class="selection-path">{{ currentSelection.display.path }}</span>
      </template>
    </el-alert>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Search } from '@element-plus/icons-vue'
import { formatLocatorDisplayPath, parseLocatorSafe } from '../types/resourceLocator.js'
import { getEngineFamily } from '../utils/engineDisplay.js'
import { engineSelectionState, isEngineSelectable } from '../utils/engineAvailability.js'
import { withTransientRetry } from '../utils/transientRequest.js'
import {
  addExpandedKey,
  defaultExpandedKeys,
  hasExpandableChildren,
  removeExpandedKey
} from '../utils/resourceTreeState.mjs'
import {
  getResourceTree,
  getResourceTreeAncestors,
  getResourceTreeNode,
  listResourceTreeEngines,
  searchResourceTree
} from '../api/resourceTree.js'
import { selectionFromResourceTreeNode } from '../utils/resourceSelection.js'
import { isResourceTreeSearchReady } from '../utils/resourceTreeSearch.mjs'
import ResourceTree from './ResourceTree.vue'

const { t } = useI18n()

const props = defineProps({
  apiBaseUrl: {
    type: String,
    default: '/api/v1/meta'
  },
  adapter: {
    type: Object,
    default: null
  },
  modelValue: {
    type: Object,
    default: null
  },
  engineId: {
    type: [Number, String, Array],
    default: null
  },
  initialLocator: {
    type: String,
    default: ''
  },
  engineFamilies: {
    type: Array,
    default: () => []
  },
  engineFilter: {
    type: Function,
    default: null
  },
  nodeFilter: {
    type: Function,
    default: null
  },
  selectableFilter: {
    type: Function,
    default: null
  },
  mode: {
    type: String,
    default: 'item',
    validator: value => ['item', 'node', 'any'].includes(value)
  },
  showEngineSelector: {
    type: Boolean,
    default: true
  },
  engineMultiple: {
    type: Boolean,
    default: false
  },
  selectAllEnginesByDefault: {
    type: Boolean,
    default: false
  },
  showSelectionSummary: {
    type: Boolean,
    default: true
  },
  showCount: {
    type: Boolean,
    default: true
  },
  showSearch: {
    type: Boolean,
    default: true
  },
  treeHeight: {
    type: String,
    default: '420px'
  },
  title: {
    type: String,
    default: ''
  },
  engineLabel: {
    type: String,
    default: '存储引擎'
  },
  enginePlaceholder: {
    type: String,
    default: '请选择存储引擎'
  },
  disabledLabel: {
    type: String,
    default: ''
  },
  showDisabledLabel: {
    type: Boolean,
    default: false
  },
  searchSelectableOnly: {
    type: Boolean,
    default: false
  },
  searchPlaceholder: {
    type: String,
    default: '搜索当前引擎资源'
  },
  searchAllEnginesPlaceholder: {
    type: String,
    default: '搜索全部引擎资源'
  },
  searchEmptyText: {
    type: String,
    default: '未找到资源'
  }
})

const emit = defineEmits([
  'update:modelValue',
  'select',
  'engine-change',
  'node-click',
  'node-expand',
  'node-dblclick',
  'error'
])

const engines = ref([])
const selectedEngineValue = ref(props.engineMultiple ? [] : null)
const treeData = ref([])
const treeRef = ref(null)
const engineSelectRef = ref(null)
const searchInputRef = ref(null)
const currentNodeKey = ref(null)
const expandedKeys = ref([])
const currentSelection = ref(props.modelValue)
const loadingEngines = ref(false)
const loadingTree = ref(false)
const searching = ref(false)
const searchKeyword = ref('')
const searchResults = ref([])
let treeLoadRequestSeq = 0
let restoreRequestSeq = 0
let searchRequestSeq = 0
let engineLoadRequestSeq = 0
let engineLoadPromise = null
let searchTimer = null

const selectedEngine = computed(() => {
  const currentEngineId = selectedEngineIds.value[0] || null
  return engines.value.find(engine => normalizeEngineId(engine.id) === currentEngineId) || null
})

const selectedEngineIds = computed(() => normalizeEngineIds(selectedEngineValue.value))
const selectedEngines = computed(() => {
  const selectedIDs = new Set(selectedEngineIds.value)
  return engines.value.filter(engine => selectedIDs.has(normalizeEngineId(engine.id)))
})

const selectedEngineId = computed(() => selectedEngineIds.value[0] || null)
const isSearchMode = computed(() => searchKeyword.value.trim().length > 0)
const activeSearchPlaceholder = computed(() => {
  if (props.engineMultiple) {
    return selectedEngineIds.value.length > 0 ? props.searchPlaceholder : props.enginePlaceholder
  }
  return selectedEngineIds.value.length > 0 ? props.searchPlaceholder : props.searchAllEnginesPlaceholder
})

const resourceTreeAdapter = computed(() => {
  if (props.adapter) {
    return props.adapter
  }
  return {
    listEngines: () => listResourceTreeEngines(props.apiBaseUrl, {
      engineFamilies: props.engineFamilies,
      engineFilter: props.engineFilter
    }),
    getTreeRoot: (engineId, options = {}) => getResourceTree(props.apiBaseUrl, engineId, options),
    getNodeChildren: async (node) => {
      const parsed = parseLocatorSafe(node.locator)
      const engineId = parsed.engineId || selectedEngineId.value
      if (!engineId) {
        return []
      }
      const result = await getResourceTreeNode(props.apiBaseUrl, engineId, node.locator)
      return result?.children || []
    },
    getAncestors: (engineId, locator) => getResourceTreeAncestors(props.apiBaseUrl, engineId, locator),
    search: (engineId, keyword, options = {}) => searchResourceTree(props.apiBaseUrl, engineId, keyword, options)
  }
})

const loadEngines = () => {
  if (engineLoadPromise) {
    return engineLoadPromise
  }
  const requestSeq = ++engineLoadRequestSeq
  loadingEngines.value = true
  const task = withTransientRetry(() => resourceTreeAdapter.value.listEngines())
    .then(result => {
      if (requestSeq !== engineLoadRequestSeq) return
      let nextEngines = normalizeEngines(result)
      if (props.adapter) {
        nextEngines = filterEngines(nextEngines)
      }
      engines.value = nextEngines
      applyDefaultEngineSelection()
    })
    .catch(error => {
      if (requestSeq === engineLoadRequestSeq) {
        handleError(error)
      }
    })
    .finally(() => {
      if (requestSeq === engineLoadRequestSeq) {
        loadingEngines.value = false
      }
      if (engineLoadPromise === task) {
        engineLoadPromise = null
      }
    })
  engineLoadPromise = task
  return task
}

const handleEngineDropdownVisible = visible => {
  if (visible) loadEngines()
}

const engineOptionLabel = engine => (
  `${engine.name} (${engine.engine_type}) · ${t(`common.engineStatus.${engineSelectionState(engine)}`)}`
)

const loadTree = async (engineId) => {
  const requestSeq = ++treeLoadRequestSeq
  if (!engineId) {
    treeData.value = []
    expandedKeys.value = []
    loadingTree.value = false
    return
  }
  loadingTree.value = true
  try {
    const root = await resourceTreeAdapter.value.getTreeRoot(engineId, { expandDepth: 1 })
    if (requestSeq !== treeLoadRequestSeq || !selectedEngineIds.value.includes(normalizeEngineId(engineId))) {
      return
    }
    const normalizedRoot = normalizeVisibleTree(root)
    upsertTreeRoot(normalizedRoot)
    expandedKeys.value = addExpandedKey(expandedKeys.value, normalizedRoot?.id)
  } catch (error) {
    if (requestSeq === treeLoadRequestSeq) {
      removeTreeRoot(engineId)
      handleError(error)
    }
  } finally {
    if (requestSeq === treeLoadRequestSeq) {
      loadingTree.value = false
    }
  }
}

const loadSelectedTrees = async () => {
  const requestSeq = ++treeLoadRequestSeq
  const selectableIDs = new Set(engines.value.filter(isEngineSelectable).map(engine => normalizeEngineId(engine.id)))
  const engineIds = selectedEngineIds.value.filter(engineID => selectableIDs.has(engineID))
  if (engineIds.length === 0) {
    treeData.value = []
    expandedKeys.value = []
    loadingTree.value = false
    return
  }
  loadingTree.value = true
  try {
    const roots = await Promise.all(engineIds.map(async engineId => {
      try {
        const root = await resourceTreeAdapter.value.getTreeRoot(engineId, { expandDepth: 1 })
        return normalizeVisibleTree(root)
      } catch (error) {
        handleError(error, { silent: engineIds.length > 1 })
        return null
      }
    }))
    if (requestSeq !== treeLoadRequestSeq) {
      return
    }
    treeData.value = roots.filter(Boolean)
    expandedKeys.value = defaultExpandedKeys(treeData.value, { expandRoot: true })
  } finally {
    if (requestSeq === treeLoadRequestSeq) {
      loadingTree.value = false
    }
  }
}

const handleEngineChange = async () => {
  restoreRequestSeq += 1
  searchRequestSeq += 1
  searching.value = false
  searchResults.value = []
  expandedKeys.value = []
  const selectionEngineId = normalizeEngineId(currentSelection.value?.identity?.engine_id)
  const shouldKeepSelection = props.engineMultiple &&
    selectionEngineId &&
    selectedEngineIds.value.includes(selectionEngineId)
  if (!shouldKeepSelection) {
    currentNodeKey.value = null
    currentSelection.value = null
    emit('update:modelValue', null)
  }
  emit('engine-change', props.engineMultiple ? selectedEngines.value : selectedEngine.value)
  await loadSelectedTrees()
  if (searchKeyword.value.trim()) {
    runSearch()
  }
}

const loadNode = async (treeNode, resolve) => {
  if (treeNode.level === 0) {
    resolve(treeData.value || [])
    return
  }
  const data = treeNode.data
  if (!data?.locator || !hasExpandableChildren(data)) {
    resolve([])
    return
  }
  try {
    const children = await resourceTreeAdapter.value.getNodeChildren(data)
    resolve(normalizeVisibleChildren(children || []))
  } catch (error) {
    handleError(error)
    resolve([])
  }
}

const handleNodeClick = (node) => {
  emit('node-click', node)
  const engine = engineForNode(node)
  if (!isSelectableNode(node, engine)) {
    return
  }
  selectNode(node, engine)
}

const handleNodeExpand = (node) => {
  emit('node-expand', node)
}

const handleNodeDblclick = (node) => {
  const engine = engineForNode(node)
  if (!isSelectableNode(node, engine)) return
  const selection = selectionFromResourceTreeNode(node, engine)
  if (!selection) return
  currentSelection.value = selection
  currentNodeKey.value = node.id
  emit('update:modelValue', selection)
  emit('select', selection)
  emit('node-dblclick', selection)
}

const handleSearchResultClick = async (result) => {
  emit('node-click', result.node)
  if (!isSelectableNode(result.node, result.engine)) {
    return
  }
  const engineId = normalizeEngineId(result.engine?.id || result.engineId)
  if (engineId && !selectedEngineIds.value.includes(engineId)) {
    selectedEngineValue.value = props.engineMultiple
      ? [...selectedEngineIds.value, engineId]
      : engineId
    emit('engine-change', result.engine || selectedEngine.value)
    await loadSelectedTrees()
  }
  selectNode(result.node, result.engine || selectedEngine.value)
}

const selectNode = (node, engine = selectedEngine.value) => {
  const selection = selectionFromResourceTreeNode(node, engine)
  currentSelection.value = selection
  currentNodeKey.value = node.id
  emit('update:modelValue', selection)
  emit('select', selection)
}

const isSelectableNode = (node, engine = selectedEngine.value) => {
  if (!node) {
    return false
  }
  const parsed = parseLocatorSafe(node.locator)
  if (!parsed.engineId) {
    return false
  }
  if (props.mode === 'item' && !parsed.itemId) {
    return false
  }
  if (props.mode === 'node' && !parsed.nodeId) {
    return false
  }
  if (typeof props.selectableFilter === 'function') {
    return props.selectableFilter(node, { engine, locator: parsed })
  }
  return true
}

const isSelectableTreeNode = (node) => {
  return isSelectableNode(node, engineForNode(node))
}

const normalizeVisibleTree = (node) => {
  if (!node) {
    return null
  }
  const normalized = {
    ...node,
    children: normalizeVisibleChildren(node.children || [])
  }
  return normalized
}

const normalizeVisibleChildren = (children) => {
  return children
    .filter(node => {
      if (typeof props.nodeFilter !== 'function') {
        return true
      }
      return props.nodeFilter(node, { engine: engineForNode(node), locator: parseLocatorSafe(node.locator) })
    })
    .map(node => normalizeVisibleTree(node))
}

const runSearch = async () => {
  const keyword = searchKeyword.value.trim()
  const requestSeq = ++searchRequestSeq
  if (!isResourceTreeSearchReady(keyword)) {
    searchResults.value = []
    searching.value = false
    return
  }
  const targetEngines = searchTargetEngines()
  if (targetEngines.length === 0) {
    searchResults.value = []
    searching.value = false
    return
  }
  searching.value = true
  try {
    const batches = await Promise.all(targetEngines.map(async engine => {
      try {
        const response = await resourceTreeAdapter.value.search?.(engine.id, keyword, { limit: 20 })
        const nodes = response?.results || response?.nodes || response || []
        return (Array.isArray(nodes) ? nodes : []).map(node => ({
          node,
          engine,
          engineId: engine.id,
          engineName: engine.name
        }))
      } catch (error) {
        handleError(error, { silent: targetEngines.length > 1 })
        return []
      }
    }))
    if (requestSeq !== searchRequestSeq) {
      return
    }
    searchResults.value = batches
      .flat()
      .filter(result => isVisibleSearchResult(result.node, result.engine))
      .slice(0, 50)
  } finally {
    if (requestSeq === searchRequestSeq) {
      searching.value = false
    }
  }
}

const isVisibleSearchResult = (node, engine) => {
  if (!node?.locator) {
    return false
  }
  if (props.searchSelectableOnly && !isSelectableNode(node, engine)) {
    return false
  }
  if (typeof props.nodeFilter !== 'function') {
    return true
  }
  return props.nodeFilter(node, { engine, locator: parseLocatorSafe(node.locator) })
}

const searchTargetEngines = () => {
  if (props.engineMultiple) {
    return selectedEngines.value
  }
  return selectedEngines.value.length > 0 ? selectedEngines.value : engines.value
}

const searchResultPath = (node, engine) => {
  const parsed = parseLocatorSafe(node?.locator || node?.id || '')
  if (parsed.path?.length) {
    return formatLocatorDisplayPath(node?.locator || node?.id || '', { engineType: engine?.engine_type })
  }
  return node?.metadata?.full_name || ''
}

const restoreInitialLocator = async (locator) => {
  const requestSeq = ++restoreRequestSeq
  searchRequestSeq += 1
  searching.value = false
  if (!locator) {
    treeLoadRequestSeq += 1
    loadingTree.value = false
    currentNodeKey.value = null
    expandedKeys.value = []
    currentSelection.value = null
    emit('update:modelValue', null)
    return
  }
  const parsed = parseLocatorSafe(locator)
  if (!parsed.engineId) {
    treeLoadRequestSeq += 1
    loadingTree.value = false
    currentNodeKey.value = null
    expandedKeys.value = []
    currentSelection.value = null
    emit('update:modelValue', null)
    return
  }
  if (props.engineMultiple) {
    const defaultEngineIds = props.selectAllEnginesByDefault
      ? engines.value.map(engine => normalizeEngineId(engine.id)).filter(Boolean)
      : []
    selectedEngineValue.value = defaultEngineIds.length > 0
      ? Array.from(new Set([...defaultEngineIds, parsed.engineId]))
      : [parsed.engineId]
    await loadSelectedTrees()
  } else {
    selectedEngineValue.value = parsed.engineId
    if (!hasLoadedRootForEngine(parsed.engineId)) {
      await loadTree(parsed.engineId)
    }
  }
  if (requestSeq !== restoreRequestSeq || !selectedEngineIds.value.includes(parsed.engineId)) {
    return
  }
  try {
    const result = await resourceTreeAdapter.value.getAncestors(parsed.engineId, locator)
    if (requestSeq !== restoreRequestSeq || !selectedEngineIds.value.includes(parsed.engineId)) {
      return
    }
    const ancestors = result?.ancestors || []
    if (ancestors.length === 0) {
      currentNodeKey.value = null
      currentSelection.value = null
      emit('update:modelValue', null)
      return
    }
    mergeAncestorChain(ancestors)
    const target = ancestors[ancestors.length - 1]
    expandedKeys.value = ancestorExpandKeys(ancestors)
    currentNodeKey.value = target.id
    const targetEngine = engineForNode(target)
    if (isSelectableNode(target, targetEngine)) {
      currentSelection.value = selectionFromResourceTreeNode(target, targetEngine)
      emit('update:modelValue', currentSelection.value)
      emit('select', currentSelection.value)
    } else {
      currentSelection.value = null
      emit('update:modelValue', null)
    }
    await nextTick()
  } catch (error) {
    handleError(error)
  }
}

const loadExternalEngine = async (engineId) => {
  restoreRequestSeq += 1
  searchRequestSeq += 1
  searching.value = false
  expandedKeys.value = []
  searchResults.value = []
  const normalizedEngineIds = normalizeEngineIds(engineId)
  if (normalizedEngineIds.length === 0) {
    treeLoadRequestSeq += 1
    loadingTree.value = false
    selectedEngineValue.value = props.engineMultiple ? [] : null
    treeData.value = []
    currentNodeKey.value = null
    currentSelection.value = null
    emit('update:modelValue', null)
    return
  }
  const loadedEngineIds = new Set(
    treeData.value
      .map(node => parseLocatorSafe(node.locator || node.id).engineId)
      .filter(Boolean)
  )
  const selectionUnchanged = normalizedEngineIds.length === selectedEngineIds.value.length &&
    normalizedEngineIds.every(id => selectedEngineIds.value.includes(id))
  const treeMatchesSelection = loadedEngineIds.size === normalizedEngineIds.length &&
    normalizedEngineIds.every(id => loadedEngineIds.has(id))
  if (selectionUnchanged && treeMatchesSelection) {
    return
  }
  selectedEngineValue.value = props.engineMultiple ? normalizedEngineIds : normalizedEngineIds[0]
  treeData.value = []
  currentNodeKey.value = null
  currentSelection.value = null
  emit('update:modelValue', null)
  await loadSelectedTrees()
  if (searchKeyword.value.trim()) {
    runSearch()
  }
}

const syncExternalSelection = async (engineId, initialLocator) => {
  const externalEngineIds = normalizeEngineIds(engineId)
  const locatorEngineId = parseLocatorSafe(initialLocator || '').engineId
  const locatorMatchesEngine = locatorEngineId && externalEngineIds.includes(locatorEngineId)

  if (initialLocator && (externalEngineIds.length === 0 || locatorMatchesEngine)) {
    await restoreInitialLocator(initialLocator)
    return
  }
  await loadExternalEngine(engineId)
}

const normalizeEngineId = (engineId) => {
  const value = Number(engineId || 0)
  return Number.isFinite(value) && value > 0 ? value : null
}

const normalizeEngineIds = (value) => {
  const values = Array.isArray(value) ? value : [value]
  return [...new Set(values.map(normalizeEngineId).filter(Boolean))]
}

const mergeAncestorChain = (ancestors) => {
  if (ancestors.length === 0) {
    return
  }
  const root = ancestors[0]
  const existingRoot = findExistingRoot(root)
  const mergedRoot = {
    ...(existingRoot || {}),
    ...root,
    children: existingRoot?.children || root.children || []
  }
  if (existingRoot) {
    Object.assign(existingRoot, mergedRoot)
  } else {
    treeData.value.push(mergedRoot)
  }
  dedupeTreeRoots()
  let cursor = existingRoot || mergedRoot
  for (const next of ancestors.slice(1)) {
    const children = cursor.children || []
    const existing = children.find(child => child.id === next.id)
    if (existing) {
      existing.children = existing.children?.length ? existing.children : (next.children || [])
      cursor = existing
    } else {
      const appended = { ...next, children: next.children || [] }
      children.push(appended)
      cursor.children = children
      cursor = appended
    }
  }
}

const ancestorExpandKeys = (ancestors) => {
  return ancestors
    .slice(0, -1)
    .map(node => node?.id)
    .filter(Boolean)
}

const hasLoadedRootForEngine = (engineId) => {
  const normalizedEngineId = normalizeEngineId(engineId)
  return treeData.value.some(node => parseLocatorSafe(node.locator || node.id).engineId === normalizedEngineId)
}

const findExistingRoot = (root) => {
  const rootKey = root.locator || root.id
  const rootEngineId = parseLocatorSafe(rootKey).engineId
  return treeData.value.find(node => {
    const nodeKey = node.locator || node.id
    if (nodeKey === rootKey) return true
    const nodeLoc = parseLocatorSafe(nodeKey)
    return rootEngineId && nodeLoc.engineId === rootEngineId && node.type === root.type
  }) || null
}

const dedupeTreeRoots = () => {
  const seen = new Set()
  treeData.value = treeData.value.filter(node => {
    const loc = parseLocatorSafe(node.locator || node.id)
    const key = loc.engineId ? `${loc.engineId}:${node.type || ''}` : (node.locator || node.id)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

const upsertTreeRoot = (root) => {
  if (!root) return
  const key = root.locator || root.id
  const existingIndex = treeData.value.findIndex(node => {
    const nodeKey = node.locator || node.id
    return nodeKey === key || findExistingRoot(root) === node
  })
  if (existingIndex >= 0) {
    treeData.value.splice(existingIndex, 1, root)
  } else {
    treeData.value.push(root)
  }
  dedupeTreeRoots()
}

const removeTreeRoot = (engineId) => {
  const normalizedEngineId = normalizeEngineId(engineId)
  const removedRootIds = treeData.value
    .filter(node => parseLocatorSafe(node.locator || node.id).engineId === normalizedEngineId)
    .map(node => node.id)
  treeData.value = treeData.value.filter(node => parseLocatorSafe(node.locator || node.id).engineId !== normalizedEngineId)
  for (const rootId of removedRootIds) {
    expandedKeys.value = removeExpandedKey(expandedKeys.value, rootId)
  }
}

const engineForNode = (node) => {
  const parsed = parseLocatorSafe(node?.locator || node?.id || '')
  return engines.value.find(engine => normalizeEngineId(engine.id) === parsed.engineId) || selectedEngine.value
}

const applyDefaultEngineSelection = () => {
  if (props.initialLocator || props.engineId) {
    return
  }
  if (props.engineMultiple) {
    if (selectedEngineIds.value.length === 0 && props.selectAllEnginesByDefault) {
      selectedEngineValue.value = engines.value.filter(isEngineSelectable).map(engine => normalizeEngineId(engine.id)).filter(Boolean)
      loadSelectedTrees()
    }
    return
  }
  if (selectedEngineId.value && engines.value.some(engine => normalizeEngineId(engine.id) === selectedEngineId.value)) {
    return
  }
  selectedEngineValue.value = null
}

const normalizeEngines = (engineList = []) => {
  return (engineList || []).map(engine => ({
    ...engine,
    id: engine.id || engine.engine_id,
    name: engine.name || engine.resource_name,
    engine_type: engine.engine_type || engine.resource_type
  }))
}

const filterEngines = (engineList = []) => {
  let nextEngines = engineList
  if (props.engineFamilies.length > 0) {
    nextEngines = nextEngines.filter(engine => props.engineFamilies.includes(getEngineFamily(engine)))
  }
  if (typeof props.engineFilter === 'function') {
    nextEngines = nextEngines.filter(props.engineFilter)
  }
  return nextEngines
}

const handleError = (error, options = {}) => {
  if (options.silent) {
    emit('error', error)
    return
  }
  ElMessage.error(error?.message || '资源树加载失败')
  emit('error', error)
}

const scheduleSearch = () => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
  searchTimer = setTimeout(() => {
    runSearch()
  }, 300)
}

watch(() => props.modelValue, value => {
  currentSelection.value = value
  currentNodeKey.value = value?.raw?.node?.id || value?.identity?.locator || null
})

watch(
  () => [props.engineId, props.initialLocator],
  ([engineId, initialLocator]) => {
    syncExternalSelection(engineId, initialLocator)
  }
)

watch(searchKeyword, () => {
  scheduleSearch()
})

onMounted(async () => {
  await loadEngines()
  if (props.initialLocator || normalizeEngineIds(props.engineId).length > 0) {
    await syncExternalSelection(props.engineId, props.initialLocator)
  }
})

onBeforeUnmount(() => {
  engineLoadRequestSeq += 1
  loadingEngines.value = false
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
})

defineExpose({
  loadEngines,
  loadTree,
  focus: async () => {
    await nextTick()
    const target = props.showSearch ? searchInputRef.value : engineSelectRef.value
    target?.focus?.()
  },
  getSelection: () => currentSelection.value
})
</script>

<style scoped>
.resource-tree-picker {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.tree-panel {
  min-height: 240px;
}

.picker-search {
  width: 100%;
}

.search-results {
  height: 100%;
  overflow: auto;
  border: 1px solid var(--addp-border-color, #dcdfe6);
  border-radius: 4px;
  padding: 8px;
}

.search-result-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.search-result {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-height: 38px;
  padding: 6px 8px;
  border: 0;
  border-radius: 4px;
  color: var(--addp-text-primary);
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.search-result:hover {
  background: var(--addp-fill-color-light, rgba(64, 158, 255, 0.1));
}

.search-result.is-disabled {
  color: var(--addp-text-tertiary);
  cursor: default;
}

.search-result-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.search-result-label,
.search-result-path,
.search-result-engine {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.search-result-label {
  font-weight: 500;
}

.search-result-path,
.search-result-engine {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.picker-node {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.picker-node.is-disabled {
  color: var(--addp-text-tertiary);
}

.picker-node-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selection-summary {
  margin-top: 4px;
}

.selection-path {
  margin-left: 8px;
  color: var(--addp-text-secondary);
  font-weight: 400;
}
</style>
