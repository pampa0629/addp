<template>
  <el-tree-select
    :model-value="modelValue"
    :data="groups"
    :default-expanded-keys="selectedCategoryKeys"
    :filter-node-method="matchesUnit"
    :value-on-clear="null"
    clearable
    filterable
    class="unit-select"
    @update:model-value="$emit('update:modelValue', $event ?? null)"
  />
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Number, default: null },
  units: { type: Array, default: () => [] }
})
defineEmits(['update:modelValue'])
const { t } = useI18n()
const groups = computed(() => {
  const categories = new Map()
  for (const unit of props.units) {
    if (!categories.has(unit.category_id)) {
      categories.set(unit.category_id, {
        value: `category:${unit.category_id}`,
        label: unit.category?.name || t('standard.common.uncategorized'),
        id: unit.category_id,
        order: unit.category?.sort_order || 0,
        children: []
      })
    }
    categories.get(unit.category_id).children.push({
      value: unit.id,
      label: unit.symbol ? `${unit.name} (${unit.symbol})` : unit.name
    })
  }
  return [...categories.values()].sort((a, b) => a.order - b.order || a.id - b.id)
})
const selectedCategoryKeys = computed(() => groups.value
  .filter(group => group.children.some(unit => unit.value === props.modelValue))
  .map(group => group.value))

function matchesUnit(query, data, node) {
  const keyword = query.trim().toLocaleLowerCase()
  return [data.label, node.parent?.data?.label]
    .some(label => label?.toLocaleLowerCase().includes(keyword))
}
</script>

<style scoped>
.unit-select { width: 100%; }
</style>
