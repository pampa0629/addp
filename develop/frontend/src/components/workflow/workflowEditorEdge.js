import G6 from '@antv/g6'

export const WORKFLOW_EDGE_TYPE = 'develop-workflow-edge'
const WORKFLOW_EDGE_CASING = 'workflow-edge-casing'

export function registerWorkflowEditorEdge(casingStyle) {
  G6.registerEdge(WORKFLOW_EDGE_TYPE, {
    afterDraw(cfg, group, keyShape) {
      if (!keyShape) return
      const casing = group.addShape('path', {
        attrs: {
          path: keyShape.attr('path'),
          stroke: casingStyle.stroke,
          lineWidth: casingStyle.lineWidth,
          lineCap: 'round',
          lineJoin: 'round'
        },
        name: WORKFLOW_EDGE_CASING,
        capture: false
      })
      group.shapeMap[WORKFLOW_EDGE_CASING] = casing
      casing.toBack()
    },

    afterUpdate(cfg, item) {
      const group = item.getContainer()
      const keyShape = item.getKeyShape()
      const casing = group.shapeMap?.[WORKFLOW_EDGE_CASING] || group.find(shape => (
        shape.get('name') === WORKFLOW_EDGE_CASING
      ))
      if (!keyShape || !casing) return
      casing.attr({
        path: keyShape.attr('path'),
        stroke: casingStyle.stroke,
        lineWidth: casingStyle.lineWidth
      })
      casing.toBack()
    }
  }, 'polyline')

  return WORKFLOW_EDGE_TYPE
}
