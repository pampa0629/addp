<template>
  <el-aside :width="sidebarWidth" :class="['sidebar', { collapsed: isCollapsed }]">
    <div class="collapse-toggle">
      <el-button
        circle
        size="small"
        :icon="isCollapsed ? Expand : Fold"
        @click="$emit('toggle-collapse')"
        :title="isCollapsed ? t('console.sidebar.expand') : t('console.sidebar.collapse')"
      />
    </div>

    <el-menu
      ref="menuRef"
      :key="activeGroupKey"
      :default-active="activeMenu"
      @select="$emit('menu-select', $event)"
      class="el-menu-vertical"
      :collapse="isCollapsed"
    >
      <template v-for="module in activeGroupModules" :key="module">
        <template v-if="sidebarMenus[module]">
          <!-- 平铺菜单项（如 agent） -->
          <el-menu-item
            v-if="sidebarMenus[module].flat"
            :index="sidebarMenus[module].index"
          >
            <el-icon><component :is="sidebarMenus[module].icon" /></el-icon>
            <span>{{ t(sidebarMenus[module].label) }}</span>
          </el-menu-item>

          <!-- 子菜单 -->
          <el-sub-menu v-else :index="module">
            <template #title>
              <el-icon><component :is="sidebarMenus[module].icon" /></el-icon>
              <span>{{ t(sidebarMenus[module].label) }}</span>
            </template>
            <template v-for="item in sidebarMenus[module].items" :key="item.index">
              <el-sub-menu v-if="item.children?.length" :index="item.index">
                <template #title>
                  <el-icon><component :is="item.icon" /></el-icon>
                  <span>{{ t(item.label) }}</span>
                </template>
                <el-menu-item
                  v-for="child in item.children"
                  :key="child.index"
                  :index="child.index"
                >
                  <el-icon><component :is="child.icon" /></el-icon>
                  <span>{{ t(child.label) }}</span>
                </el-menu-item>
              </el-sub-menu>
              <el-menu-item
                v-else
                :index="item.index"
              >
                <el-icon><component :is="item.icon" /></el-icon>
                <span>{{ t(item.label) }}</span>
              </el-menu-item>
            </template>
          </el-sub-menu>
        </template>
      </template>
    </el-menu>
  </el-aside>
</template>

<script setup>
import { ref, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Fold, Expand } from '@element-plus/icons-vue'

const { t } = useI18n()

const props = defineProps({
  activeGroupModules: { type: Array, required: true },
  activeGroupKey: { type: String, default: null },
  activeMenu: { type: String, default: '/' },
  isCollapsed: { type: Boolean, default: false },
  sidebarMenus: { type: Object, required: true },
})

defineEmits(['menu-select', 'toggle-collapse'])

const menuRef = ref(null)

const sidebarWidth = computed(() => (props.isCollapsed ? '72px' : '240px'))

defineExpose({
  async openModule(module, groupModules) {
    if (!menuRef.value) return
    await nextTick()
    const isSubMenu = !props.sidebarMenus[module]?.flat
    groupModules.forEach(m => {
      if (m !== module && !props.sidebarMenus[m]?.flat) menuRef.value.close(m)
    })
    if (isSubMenu) menuRef.value.open(module)
  },
})
</script>

<style scoped>
.sidebar {
  background: var(--addp-bg-sidebar);
  border-right: 1px solid var(--addp-border-color);
  display: flex;
  flex-direction: column;
  transition: width 0.2s ease;
}

.sidebar.collapsed {
  align-items: center;
}

.collapse-toggle {
  display: flex;
  justify-content: flex-end;
  padding: 12px 12px 6px;
}

.sidebar.collapsed .collapse-toggle {
  justify-content: center;
}

.collapse-toggle :deep(.el-button) {
  border: none;
}

.sidebar.collapsed :deep(.el-menu-vertical) {
  border-right: none;
}

.el-menu-vertical {
  border-right: none;
}

.sidebar :deep(.el-menu-item-group__title) {
  font-size: 14px;
  font-weight: 600;
  padding: 12px 0 8px 20px;
  color: var(--addp-text-secondary);
}

.sidebar.collapsed :deep(.el-menu-item-group__title) {
  padding-left: 0;
  text-align: center;
}
</style>
