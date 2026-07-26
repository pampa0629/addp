import G6 from '@antv/g6'
import { isWorkflowInputParameter } from '@/utils/workflowInputBindings'

const NODE_TYPE = 'develop-workflow-node'
const NODE_WIDTH = 220
const HEADER_HEIGHT = 36
const PORT_ROW_HEIGHT = 24

export function operatorInputPorts(operator) {
  return (operator?.public_parameters || [])
    .filter(isWorkflowInputParameter)
    .map(parameter => ({
      name: parameter.name,
      type: parameter.type,
      required: Boolean(parameter.required),
      description: parameter.description || ''
    }))
}

export function operatorOutputPorts(operator) {
  return (operator?.output_ports || []).map(port => ({
    name: port.name,
    type: port.type,
    is_default: Boolean(port.is_default),
    description: port.description || ''
  }))
}

export function registerWorkflowEditorNode() {
  if (G6.getNodeType?.(NODE_TYPE)) return NODE_TYPE

  G6.registerNode(NODE_TYPE, {
    draw(cfg, group) {
      const inputPorts = cfg.inputPorts || []
      const outputPorts = cfg.outputPorts || []
      const portRows = Math.max(inputPorts.length, outputPorts.length, 1)
      const title = cfg.displayName || cfg.label || cfg.operator
      const height = HEADER_HEIGHT + 18 + portRows * PORT_ROW_HEIGHT
      const colors = canvasThemeColors()

      const rect = group.addShape('rect', {
        attrs: {
          x: -NODE_WIDTH / 2,
          y: -height / 2,
          width: NODE_WIDTH,
          height,
          radius: 6,
          fill: colors.background,
          stroke: colors.border,
          lineWidth: 1.5,
          cursor: 'move'
        },
        name: 'workflow-node-card',
        draggable: true
      })

      group.addShape('text', {
        attrs: {
          text: title,
          x: -NODE_WIDTH / 2 + 14,
          y: -height / 2 + HEADER_HEIGHT / 2,
          textAlign: 'left',
          textBaseline: 'middle',
          fill: colors.textPrimary,
          fontSize: 13,
          fontWeight: 600
        },
        name: 'workflow-node-title',
        capture: false
      })

      group.addShape('line', {
        attrs: {
          x1: -NODE_WIDTH / 2,
          y1: -height / 2 + HEADER_HEIGHT,
          x2: NODE_WIDTH / 2,
          y2: -height / 2 + HEADER_HEIGHT,
          stroke: colors.border,
          lineWidth: 1
        },
        name: 'workflow-node-divider',
        capture: false
      })

      inputPorts.forEach((port, index) => {
        const y = portY(height, HEADER_HEIGHT, index)
        drawPort(group, {
          x: -NODE_WIDTH / 2,
          y,
          port,
          portType: 'input',
          colors
        })
        drawPortLabel(group, {
          text: port.name,
          x: -NODE_WIDTH / 2 + 13,
          y,
          align: 'left',
          colors
        })
      })

      outputPorts.forEach((port, index) => {
        const y = portY(height, HEADER_HEIGHT, index)
        drawPort(group, {
          x: NODE_WIDTH / 2,
          y,
          port,
          portType: 'output',
          colors
        })
        drawPortLabel(group, {
          text: port.name,
          x: NODE_WIDTH / 2 - 13,
          y,
          align: 'right',
          colors
        })
      })

      return rect
    },

    getAnchorPoints(cfg) {
      const inputPorts = cfg.inputPorts || []
      const outputPorts = cfg.outputPorts || []
      const rows = Math.max(inputPorts.length, outputPorts.length, 1)
      const height = HEADER_HEIGHT + 18 + rows * PORT_ROW_HEIGHT
      return [
        ...inputPorts.map((_, index) => [0, normalizedPortY(height, HEADER_HEIGHT, index)]),
        ...outputPorts.map((_, index) => [1, normalizedPortY(height, HEADER_HEIGHT, index)])
      ]
    }
  }, 'single-node')

  return NODE_TYPE
}

function portY(height, headerHeight, index) {
  return -height / 2 + headerHeight + 18 + index * PORT_ROW_HEIGHT
}

function normalizedPortY(height, headerHeight, index) {
  return (portY(height, headerHeight, index) + height / 2) / height
}

function drawPort(group, { x, y, port, portType, colors }) {
  group.addShape('circle', {
    attrs: {
      x,
      y,
      r: 6,
      fill: colors.background,
      stroke: portType === 'input' ? colors.success : colors.primary,
      lineWidth: 2,
      cursor: 'crosshair'
    },
    name: `${portType}-port-${port.name}`,
    draggable: true,
    portType,
    portName: port.name,
    portDataType: port.type,
    portDescription: port.description
  })
}

function drawPortLabel(group, { text, x, y, align, colors }) {
  group.addShape('text', {
    attrs: {
      text,
      x,
      y,
      textAlign: align,
      textBaseline: 'middle',
      fill: colors.textSecondary,
      fontSize: 10
    },
    name: `port-label-${text}`,
    capture: false
  })
}

function canvasThemeColors() {
  const style = getComputedStyle(document.documentElement)
  return {
    background: style.getPropertyValue('--addp-bg-primary').trim(),
    border: style.getPropertyValue('--addp-border-color').trim(),
    textPrimary: style.getPropertyValue('--addp-text-primary').trim(),
    textSecondary: style.getPropertyValue('--addp-text-secondary').trim(),
    primary: style.getPropertyValue('--el-color-primary').trim(),
    success: style.getPropertyValue('--el-color-success').trim()
  }
}
