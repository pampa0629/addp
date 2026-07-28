<template>
  <el-header class="header">
    <div class="header-left">
      <div class="logo-area" @click="$emit('logo-click')">
        <el-icon :size="28" style="margin-right: 12px"><Platform /></el-icon>
        <h1>{{ t('console.title') }}</h1>
      </div>

      <!-- 全局搜索 -->
      <div class="search-wrap" ref="searchWrapRef">
        <div class="search-input-box" :class="{ 'is-focused': searchFocused }">
          <el-icon class="search-icon"><Search /></el-icon>
          <input
            ref="searchInputRef"
            v-model="searchQuery"
            class="search-input"
            :placeholder="t('console.search.placeholder')"
            @focus="searchFocused = true"
            @blur="handleSearchBlur"
            @keydown.esc="clearSearch"
            @keydown.enter="handleSearchEnter"
            @keydown.down.prevent="moveSelection(1)"
            @keydown.up.prevent="moveSelection(-1)"
          />
          <el-icon v-if="searchQuery" class="search-clear" @mousedown.prevent="clearSearch"><Close /></el-icon>
        </div>

        <!-- 搜索结果下拉 -->
        <div v-if="searchFocused && searchResults.length > 0" class="search-dropdown">
          <div
            v-for="(item, idx) in searchResults"
            :key="item.route"
            class="search-result-item"
            :class="{ 'is-selected': idx === selectedIndex }"
            @mousedown.prevent="handleResultClick(item)"
          >
            <el-icon :size="14" class="result-icon"><component :is="moduleIcon(item.module)" /></el-icon>
            <span class="result-label">{{ item.label }}</span>
            <span class="result-module">{{ t(`console.modules.${item.module}.label`) }}</span>
          </div>
        </div>
        <div v-else-if="searchFocused && searchQuery && searchResults.length === 0" class="search-dropdown">
          <div class="search-empty">{{ t('console.search.noResult') }}</div>
        </div>
      </div>

      <nav class="group-tabs">
        <el-tooltip
          v-for="group in groups"
          :key="group.key"
          :content="t(group.label)"
          placement="bottom"
          :show-after="200"
        >
          <button
            class="group-tab"
            :class="{ 'is-active': activeGroup === group.key }"
            @click="$emit('group-click', group)"
          >
            <el-icon :size="20"><component :is="group.icon" /></el-icon>
          </button>
        </el-tooltip>
      </nav>
    </div>

    <div class="header-right">
      <ThemeSwitcher class="header-theme" />
      <LangSwitcher class="header-language" />

      <el-dropdown>
        <span class="user-dropdown">
          <el-icon><User /></el-icon>
          <span class="user-name">{{ userDisplayName }}</span>
          <el-icon class="el-icon--right"><ArrowDown /></el-icon>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="$emit('logout')">
              <el-icon><SwitchButton /></el-icon>
              {{ t('console.logout') }}
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </el-header>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Platform, User, ArrowDown, SwitchButton, Search, Close } from '@element-plus/icons-vue'
import {
  Upload, Box, DataAnalysis, Link, Edit, Share, CircleCheck, Operation,
  Connection, Setting, ChatDotRound, Folder, Reading, Grid,
} from '@element-plus/icons-vue'
import ThemeSwitcher from '../ThemeSwitcher.vue'
import LangSwitcher from '../LangSwitcher.vue'
import { searchIndex } from '../../config/searchIndex'

const { t } = useI18n()

const props = defineProps({
  groups: { type: Array, required: true },
  activeGroup: { type: String, default: null },
  user: { type: Object, default: null },
  permissions: { type: Array, default: () => [] },
})

const emit = defineEmits(['group-click', 'logo-click', 'logout', 'navigate'])
const userDisplayName = computed(() =>
  props.user?.display_name ||
  props.user?.local_account?.username ||
  t('console.welcome.defaultName')
)

// ─── 搜索 ────────────────────────────────────────────────────────────────────

const searchQuery = ref('')
const searchFocused = ref(false)
const selectedIndex = ref(-1)
const searchInputRef = ref(null)

const MODULE_ICONS = {
  transfer: Upload, meta: Box, manager: DataAnalysis, standard: Reading,
  modeling: Grid, quality: CircleCheck, develop: Edit, service: Link,
  orchestrator: Operation, monitor: DataAnalysis, asset: Folder,
  agent: ChatDotRound, graph: Share, system: Setting,
}

function moduleIcon(module) {
  return MODULE_ICONS[module] || Connection
}

const searchResults = computed(() => {
  if (!searchQuery.value.trim()) return []
  return searchIndex(searchQuery.value, t, props.permissions)
})

function handleSearchBlur() {
  // 延迟关闭，让 mousedown 事件先触发
  setTimeout(() => { searchFocused.value = false }, 150)
}

function clearSearch() {
  searchQuery.value = ''
  selectedIndex.value = -1
  searchInputRef.value?.focus()
}

function handleSearchEnter() {
  if (selectedIndex.value >= 0 && searchResults.value[selectedIndex.value]) {
    handleResultClick(searchResults.value[selectedIndex.value])
  } else if (searchResults.value.length > 0) {
    handleResultClick(searchResults.value[0])
  }
}

function moveSelection(dir) {
  const len = searchResults.value.length
  if (len === 0) return
  selectedIndex.value = (selectedIndex.value + dir + len) % len
}

function handleResultClick(item) {
  emit('navigate', item.route)
  searchQuery.value = ''
  searchFocused.value = false
  selectedIndex.value = -1
}
</script>

<style scoped>
.header {
  background: var(--addp-bg-header);
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  gap: 12px;
}

.logo-area {
  display: flex;
  align-items: center;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: background 0.2s;
  flex-shrink: 0;
}

.logo-area:hover {
  background: var(--addp-bg-secondary);
}

.logo-area h1 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
  background: var(--addp-primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  white-space: nowrap;
}

/* 搜索框 */
.search-wrap {
  position: relative;
  flex: 1;
  max-width: 320px;
  min-width: 160px;
}

.search-input-box {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  padding: 0 10px;
  height: 34px;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.search-input-box.is-focused {
  border-color: var(--el-color-primary-light-5);
  box-shadow: 0 0 0 2px var(--el-color-primary-light-9);
}

.search-icon {
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
}

.search-input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  font-size: 13px;
  color: var(--addp-text-primary);
  min-width: 0;
}

.search-input::placeholder {
  color: var(--addp-text-tertiary);
}

.search-clear {
  color: var(--addp-text-tertiary);
  cursor: pointer;
  flex-shrink: 0;
  transition: color 0.15s;
}

.search-clear:hover {
  color: var(--addp-text-secondary);
}

.search-dropdown {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 10px;
  box-shadow: var(--addp-shadow-hover);
  z-index: 9999;
  overflow: hidden;
}

.search-result-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 14px;
  cursor: pointer;
  transition: background 0.15s;
}

.search-result-item:hover,
.search-result-item.is-selected {
  background: var(--addp-bg-secondary);
}

.result-icon {
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
}

.result-label {
  flex: 1;
  font-size: 13px;
  color: var(--addp-text-primary);
}

.result-module {
  font-size: 11px;
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
}

.search-empty {
  padding: 16px;
  text-align: center;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

/* 群组 Tab */
.group-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
  flex-shrink: 0;
}

.group-tabs::-webkit-scrollbar {
  display: none;
}

.group-tab {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--addp-text-secondary);
  cursor: pointer;
  border-radius: 8px;
  transition: background 0.2s, color 0.2s;
  flex-shrink: 0;
}

.group-tab:hover {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
}

.group-tab.is-active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.header-right {
  display: flex;
  align-items: center;
}

.header-theme { margin-right: 8px; }
.header-language { margin-right: 16px; }

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: 4px;
  transition: background 0.3s;
}

.user-dropdown:hover {
  background: var(--addp-bg-secondary);
}

@media (max-width: 760px) {
  .header { height: 52px; padding: 0 8px; }
  .header-left { flex: 0 1 auto; gap: 4px; }
  .logo-area { padding: 4px; }
  .logo-area h1, .search-wrap, .group-tabs, .header-theme, .header-language, .user-name { display: none; }
  .user-dropdown { padding: 8px; }
}
</style>
