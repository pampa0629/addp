import { computed, ref, watch } from 'vue'
import { useConsolePageDescriptor } from '@common-ui'
import { knowledgeGraphAPI } from '../api/ontology'

export function useKnowledgeGraphPageDescriptor(router, graphId, title) {
  const graph = ref(null)

  watch(
    () => String(graphId.value || '').trim(),
    async id => {
      graph.value = null
      if (!id) return
      try {
        graph.value = await knowledgeGraphAPI.get(id)
      } catch {
        graph.value = null
      }
    },
    { immediate: true }
  )

  useConsolePageDescriptor(router, 'graph', {
    title,
    subject: computed(() => graph.value?.name || ''),
    ready: computed(() => Boolean(graph.value?.name))
  })

  return graph
}
