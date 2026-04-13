import { ref, onBeforeUnmount } from 'vue'

/**
 * 可拖拽调整大小的 Composable
 * @param {number} initialSize - 初始大小
 * @param {number} minSize - 最小大小
 * @param {number} maxSize - 最大大小
 * @param {string} direction - 方向 ('horizontal' | 'vertical')
 */
export function useResizable(initialSize, minSize, maxSize, direction = 'horizontal') {
  const size = ref(initialSize)
  const isResizing = ref(false)

  let startPosition = 0
  let startSize = 0

  const onResize = (event) => {
    if (!isResizing.value) return

    const delta = direction === 'horizontal'
      ? event.clientX - startPosition
      : event.clientY - startPosition

    const nextSize = Math.min(maxSize, Math.max(minSize, startSize + delta))
    size.value = nextSize
  }

  const resizeClass = direction === 'horizontal' ? 'is-h-resizing' : 'is-v-resizing'

  const stopResize = () => {
    if (!isResizing.value) return
    isResizing.value = false
    document.body.classList.remove(resizeClass)
    document.body.style.userSelect = ''
    document.body.style.cursor = ''
    document.removeEventListener('mousemove', onResize)
    document.removeEventListener('mouseup', stopResize)
  }

  const startResize = (event) => {
    isResizing.value = true
    startPosition = direction === 'horizontal' ? event.clientX : event.clientY
    startSize = size.value
    document.body.classList.add(resizeClass)
    document.body.style.userSelect = 'none'
    document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize'
    document.addEventListener('mousemove', onResize)
    document.addEventListener('mouseup', stopResize)
  }

  onBeforeUnmount(() => {
    stopResize()
  })

  return {
    size,
    isResizing,
    startResize,
    stopResize
  }
}
