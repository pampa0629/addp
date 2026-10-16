<template>
  <el-collapse-item
    class="operator-category"
    :class="{ 'is-root-category': category.path.length === 1, 'is-nested-category': category.path.length > 1 }"
    :name="category.key"
    :data-category-path="JSON.stringify(category.path)"
  >
    <template #title>
      <div class="category-header">
        <el-icon class="category-icon" aria-hidden="true">
          <FolderOpened v-if="category.children.length && activeCategories.includes(category.key)" />
          <Folder v-else-if="category.path.length === 1 || category.children.length" />
          <CollectionTag v-else />
        </el-icon>
        <span class="category-name">{{ category.name }}</span>
        <el-tag size="small" type="info" class="category-count">{{ category.count }}</el-tag>
      </div>
    </template>
    <el-collapse
      v-if="category.children.length"
      v-model="activeCategories"
      class="nested-categories"
    >
      <OperatorPaletteCategory
        v-for="child in category.children"
        :key="child.key"
        v-model:active-categories="activeCategories"
        :category="child"
        @operator-click="handleOperatorClick"
        @operator-drag-start="handleDragStart"
      />
    </el-collapse>
    <!-- 算子列表 -->
    <div v-if="category.operators.length" class="operator-list">
      <div
        v-for="operator in category.operators"
        :key="operator.name"
        class="operator-item"
        draggable="true"
        @dragstart="handleDragStart($event, operator)"
      >
        <button
          type="button"
          class="operator-add-button"
          :aria-label="t('develop.operatorPalette.addLabel', { name: operator.displayName })"
          @click="handleOperatorClick(operator)"
        >
          <span class="operator-name">{{ operator.displayName }}</span>
          <span
            v-if="hasDistinctText(operator.description, operator.displayName, operator.name)"
            class="operator-description"
          >
            {{ operator.description }}
          </span>
        </button>
        <el-tooltip effect="light" placement="right" :show-after="300">
          <template #content>
            <div class="operator-help">
              <h4>{{ operator.displayName }}</h4>
              <p
                v-if="hasDistinctText(operator.description, operator.displayName, operator.name)"
                class="description"
              >
                {{ operator.description }}
              </p>
              <div class="params-section" v-if="operator.params && Object.keys(operator.params).length > 0">
                <h5>{{ t('develop.operatorPalette.params') }}:</h5>
                <ul>
                  <li v-for="(desc, paramName) in operator.params" :key="paramName">
                    <strong>{{ paramName }}</strong>: {{ desc }}
                  </li>
                </ul>
              </div>
            </div>
          </template>
          <button
            type="button"
            class="info-button"
            :aria-label="t('develop.operatorPalette.helpLabel', { name: operator.displayName })"
            @click.stop
          >
            <el-icon class="info-icon"><InfoFilled /></el-icon>
          </button>
        </el-tooltip>
      </div>
    </div>
  </el-collapse-item>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { CollectionTag, Folder, FolderOpened, InfoFilled } from '@element-plus/icons-vue'
import { hasDistinctText } from '@/utils/displayText'

const { t } = useI18n()
defineProps({ category: { type: Object, required: true } })
const activeCategories = defineModel('activeCategories', { type: Array, default: () => [] })
const emit = defineEmits(['operator-click', 'operator-drag-start'])

function handleOperatorClick(operator) {
  emit('operator-click', operator)
}

function handleDragStart(event, operator) {
  emit('operator-drag-start', event, operator)
}
</script>

<style scoped>
.operator-category {
  position: relative;
}

.operator-category > :deep(.el-collapse-item__header) {
  height: 40px;
  padding: 0 10px;
  color: var(--addp-text-primary);
  line-height: 20px;
  text-align: left;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.operator-category > :deep(.el-collapse-item__header:focus-visible) {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}

.operator-category > :deep(.el-collapse-item__header > .el-collapse-item__arrow) {
  order: -1;
  margin: 0 6px 0 0;
  font-size: 12px;
  flex-shrink: 0;
}

.operator-category > :deep(.el-collapse-item__wrap) {
  border-bottom: 0;
  background: transparent;
}

.operator-category > :deep(.el-collapse-item__wrap > .el-collapse-item__content) {
  padding-bottom: 4px;
}

.is-root-category {
  margin-bottom: 8px;
}

.is-root-category > :deep(.el-collapse-item__header[aria-expanded='true']) {
  box-shadow: inset 3px 0 0 var(--el-color-primary);
}

.is-nested-category::before {
  position: absolute;
  top: 16px;
  left: -12px;
  width: 12px;
  content: '';
  border-top: 1px solid var(--addp-border-color);
}

.is-nested-category > :deep(.el-collapse-item__header) {
  height: 32px;
  padding: 0 6px;
  border: 0;
  border-radius: 4px;
  background: transparent;
}

.is-nested-category > :deep(.el-collapse-item__header:hover) {
  background: var(--addp-bg-secondary);
}

.nested-categories {
  margin: 4px 0 4px 17px;
  padding-left: 12px;
  border-top: 0;
  border-bottom: 0;
  border-left: 1px solid var(--addp-border-color);
}

.category-header {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  justify-content: flex-start;
  text-align: left;
}

.category-icon {
  flex-shrink: 0;
  color: var(--el-color-primary);
  font-size: 16px;
}

.category-name {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.is-nested-category > :deep(.el-collapse-item__header) .category-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--addp-text-secondary);
}

.is-nested-category > :deep(.el-collapse-item__header) .category-icon {
  color: var(--addp-text-tertiary);
  font-size: 14px;
}

.category-count {
  flex-shrink: 0;
  margin-left: 0;
}

.operator-list {
  padding: 4px 0 8px 24px;
}

.operator-item {
  padding: 8px 10px;
  margin: 4px 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 24px;
  align-items: start;
  gap: 8px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  cursor: grab;
  transition: all 0.2s;
}

.operator-item:hover {
  background: var(--addp-bg-secondary);
  border-color: var(--el-color-primary);
  transform: translateX(4px);
}

.operator-item:active {
  cursor: grabbing;
}

.operator-add-button {
  width: 100%;
  min-width: 0;
  padding: 0;
  color: inherit;
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
  border-radius: 2px;
  cursor: pointer;
}

.operator-add-button:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 3px;
}

.operator-name {
  display: block;
  min-width: 0;
  overflow: hidden;
  font-weight: 600;
  color: var(--addp-text-primary);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.info-button {
  width: 24px;
  height: 24px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--addp-text-tertiary);
  font: inherit;
  background: transparent;
  border: 0;
  border-radius: 4px;
  cursor: help;
}

.info-button:hover {
  color: var(--el-color-primary);
  background: var(--addp-bg-primary);
}

.info-button:focus-visible {
  color: var(--el-color-primary);
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}

.info-icon {
  font-size: 16px;
  pointer-events: none;
}

.operator-description {
  margin-top: 4px;
  display: block;
  font-size: 12px;
  color: var(--addp-text-secondary);
  line-height: 1.4;
}

/* Tooltip 帮助内容 */
.operator-help {
  max-width: 400px;
}

.operator-help h4 {
  margin: 0 0 8px 0;
  font-size: 16px;
  color: var(--addp-text-primary);
}

.operator-help .description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.operator-help .params-section h5 {
  margin: 0 0 8px 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.operator-help .params-section ul {
  margin: 0;
  padding-left: 20px;
}

.operator-help .params-section li {
  margin: 4px 0;
  font-size: 13px;
  color: var(--addp-text-secondary);
  line-height: 1.4;
}

.operator-help .params-section strong {
  color: var(--el-color-primary);
}

</style>
