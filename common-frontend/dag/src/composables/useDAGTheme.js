import { onMounted, onUnmounted } from 'vue'

export function useDAGTheme(refresh) {
  let observer
  onMounted(() => {
    observer = new MutationObserver(refresh)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
  })
  onUnmounted(() => observer?.disconnect())
}
