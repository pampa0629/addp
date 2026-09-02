import { ref, onBeforeUnmount } from 'vue'

/**
 * 可拖拽调整大小的 Composable
 * @param {number} initialSize - 初始大小
 * @param {number} minSize - 最小大小
 * @param {number} maxSize - 最大大小
 * @param {string} direction - 方向 ('horizontal' | 'vertical')
 * @param {{ reverse?: boolean }} options - 是否反向计算拖拽增量（用于右侧面板的左边缘）
 */
export function useResizable(initialSize, minSize, maxSize, direction = 'horizontal', options = {}) {
  const normalizedInitialSize = Math.min(maxSize, Math.max(minSize, initialSize))
  const size = ref(normalizedInitialSize)
  const isResizing = ref(false)
  const directionFactor = options.reverse === true ? -1 : 1

  let startPosition = 0
  let startSize = 0

  const setSize = (nextSize) => {
    size.value = Math.min(maxSize, Math.max(minSize, nextSize))
  }

  const onResize = (event) => {
    if (!isResizing.value) return

    const delta = direction === 'horizontal'
      ? event.clientX - startPosition
      : event.clientY - startPosition

    setSize(startSize + (delta * directionFactor))
  }

  const resizeClass = direction === 'horizontal' ? 'is-h-resizing' : 'is-v-resizing'

  const handleResizeKeydown = (event, step = 16) => {
    const key = event?.key
    let nextSize = null

    if (key === 'Home') nextSize = minSize
    else if (key === 'End') nextSize = maxSize
    else if (direction === 'horizontal' && key === 'ArrowLeft') nextSize = size.value - (step * directionFactor)
    else if (direction === 'horizontal' && key === 'ArrowRight') nextSize = size.value + (step * directionFactor)
    else if (direction === 'vertical' && key === 'ArrowUp') nextSize = size.value - (step * directionFactor)
    else if (direction === 'vertical' && key === 'ArrowDown') nextSize = size.value + (step * directionFactor)

    if (nextSize === null) return false
    event.preventDefault?.()
    setSize(nextSize)
    return true
  }

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
    minSize,
    maxSize,
    setSize,
    resetSize: () => setSize(normalizedInitialSize),
    startResize,
    handleResizeKeydown,
    stopResize
  }
}
