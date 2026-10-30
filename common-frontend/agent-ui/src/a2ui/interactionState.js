import { inject, ref } from 'vue'

export const interactionDisabledKey = Symbol('a2ui-interaction-disabled')

export function useInteractionDisabled() {
  return inject(interactionDisabledKey, ref(false))
}
