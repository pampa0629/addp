/** 原生元素只适配 JSON attribute 与高度，画布由共享 DAGViewer 唯一拥有。 */
import { computed, defineCustomElement, h } from 'vue'
import DAGViewer from '../components/DAGViewer.vue'
import viewerStyles from '../components/dag-viewer.css?inline'

const DAGViewerElement = defineCustomElement({
  styles: [viewerStyles],
  props: {
    dagData: { type: String, default: '' },
    height: { type: Number, default: 400 }
  },
  setup(props) {
    const data = computed(() => {
      try { return JSON.parse(props.dagData) || { tasks: [] } }
      catch { return { tasks: [] } }
    })
    return () => h(DAGViewer, { dagData: data.value, height: props.height })
  }
})

export function registerDagViewerElement() {
  if (!customElements.get('addp-dag-viewer')) customElements.define('addp-dag-viewer', DAGViewerElement)
}
