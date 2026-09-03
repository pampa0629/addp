<template>
  <div class="data-pagination">
    <el-pagination
      :background="background"
      :size="size || undefined"
      :layout="layout"
      :total="total"
      :page-size="pageSize"
      :current-page="currentPage"
      :page-sizes="pageSizes"
      :pager-count="pagerCount"
      @update:current-page="handleCurrentChange"
      @update:page-size="handlePageSizeChange"
    />
    <div v-if="$slots.default" class="data-pagination__aside">
      <slot />
    </div>
  </div>
</template>

<script setup>
const props = defineProps({
  total: { type: Number, default: 0 },
  currentPage: { type: Number, default: 1 },
  pageSize: { type: Number, default: 20 },
  pageSizes: { type: Array, default: () => [10, 20, 50, 100] },
  layout: { type: String, default: 'total, sizes, prev, pager, next, jumper' },
  pagerCount: { type: Number, default: 7 },
  background: { type: Boolean, default: true },
  size: { type: String, default: '' }
})

const emit = defineEmits(['update:currentPage', 'update:pageSize', 'change'])

const handleCurrentChange = (page) => {
  emit('update:currentPage', page)
  emit('change', { page, pageSize: props.pageSize })
}

const handlePageSizeChange = (pageSize) => {
  emit('update:currentPage', 1)
  emit('update:pageSize', pageSize)
  emit('change', { page: 1, pageSize })
}
</script>

<style scoped>
.data-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.data-pagination :deep(.el-pagination) {
  max-width: 100%;
  overflow-x: auto;
}

.data-pagination__aside {
  flex: 0 0 auto;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

@media (max-width: 768px) {
  .data-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
