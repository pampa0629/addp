import { defineComponent, h, onUnmounted, ref } from 'vue'
import { ComponentContext } from '@a2ui/web_core/v0_9'

const A2UIComponentNode = defineComponent({
  name: 'A2UIComponentNode',
  props: {
    surface: { type: Object, required: true },
    componentId: { type: String, required: true },
    basePath: { type: String, default: '/' }
  },
  setup(props) {
    const version = ref(0)
    const subscriptions = [
      props.surface.componentsModel.onCreated.subscribe(component => {
        if (component.id === props.componentId) version.value += 1
      }),
      props.surface.componentsModel.onDeleted.subscribe(componentId => {
        if (componentId === props.componentId) version.value += 1
      })
    ]

    onUnmounted(() => subscriptions.forEach(subscription => subscription.unsubscribe()))

    const buildChild = (componentId, basePath = props.basePath) => h(A2UIComponentNode, {
      key: `${componentId}:${basePath}`,
      surface: props.surface,
      componentId,
      basePath
    })

    return () => {
      void version.value
      const component = props.surface.componentsModel.get(props.componentId)
      if (!component) return null
      const implementation = props.surface.catalog.components.get(component.type)
      if (!implementation?.render) return null
      const context = new ComponentContext(props.surface, props.componentId, props.basePath)
      return h(implementation.render, { context, buildChild })
    }
  }
})

export default defineComponent({
  name: 'A2UIRoot',
  props: {
    surface: { type: Object, required: true }
  },
  setup(props) {
    return () => h(A2UIComponentNode, {
      surface: props.surface,
      componentId: 'root',
      basePath: '/'
    })
  }
})
