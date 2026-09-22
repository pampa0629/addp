import { computed, ref, toValue } from 'vue'
import { useRouter } from 'vue-router'
import { useUnsavedChangesGuard } from '@common-ui'

export const snapshotUnsavedState = (state) => JSON.stringify(state ?? null)

export function useUnsavedChanges({ state }) {
  const savedSnapshots = ref({})
  const ready = ref(false)
  const currentSnapshots = computed(() => Object.fromEntries(
    Object.entries(toValue(state)).map(([key, value]) => [key, snapshotUnsavedState(value)])
  ))
  const isDirty = computed(() => ready.value && Object.keys(currentSnapshots.value)
    .some(key => savedSnapshots.value[key] !== currentSnapshots.value[key]))

  const markSaved = (section) => {
    savedSnapshots.value = section
      ? { ...savedSnapshots.value, [section]: currentSnapshots.value[section] }
      : { ...currentSnapshots.value }
    ready.value = true
  }

  useUnsavedChangesGuard({
    router: useRouter(),
    isDirty: () => isDirty.value
  })

  return { isDirty, markSaved }
}
