<template>
  <div
    class="splitter"
    :class="{ horizontal: direction === 'horizontal', vertical: direction === 'vertical' }"
    @mousedown="handleMouseDown"
  ></div>
</template>

<script setup>
const props = defineProps({
  direction: {
    type: String,
    default: 'horizontal', // 'horizontal' | 'vertical'
    validator: (value) => ['horizontal', 'vertical'].includes(value)
  }
})

const emit = defineEmits(['resize'])

const handleMouseDown = (event) => {
  emit('resize', event)
}
</script>

<style scoped>
.splitter {
  position: relative;
}

.splitter.horizontal {
  cursor: col-resize;
  width: 8px;
  height: 100%;
}

.splitter.vertical {
  cursor: row-resize;
  height: 8px;
  width: 100%;
}

.splitter.horizontal::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 2px;
  border-radius: 1px;
  background: var(--el-color-primary-light-9);
}

.splitter.vertical::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 2px;
  border-radius: 1px;
  background: var(--el-color-primary-light-9);
}

.splitter:hover::after,
body.is-resizing .splitter::after {
  background: var(--el-color-primary);
}
</style>
