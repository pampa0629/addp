import G6 from '@antv/g6'
import { isWorkflowInputParameter } from '@/utils/workflowInputBindings'

const NODE_TYPE = 'develop-workflow-node'
const NODE_WIDTH = 220
const HEADER_HEIGHT = 36
const PORT_ROW_HEIGHT = 24
const SUMMARY_LINE_HEIGHT = 16
const MAX_SUMMARY_ITEMS = 2
const MAX_SUMMARY_LINES = 5

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
      const layout = nodeLayout(cfg)
      const colors = canvasThemeColors()

      const rect = group.addShape('rect', {
        attrs: {
          x: -NODE_WIDTH / 2,
          y: -layout.height / 2,
          width: NODE_WIDTH,
          height: layout.height,
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
          text: cfg.displayName || cfg.label || cfg.operator,
          x: -NODE_WIDTH / 2 + 14,
          y: -layout.height / 2 + HEADER_HEIGHT / 2,
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
          y1: -layout.height / 2 + HEADER_HEIGHT,
          x2: NODE_WIDTH / 2,
          y2: -layout.height / 2 + HEADER_HEIGHT,
          stroke: colors.border,
          lineWidth: 1
        },
        name: 'workflow-node-divider',
        capture: false
      })

      for (let index = 0; index < MAX_SUMMARY_LINES; index += 1) {
        const line = layout.summaryLines[index]
        group.addShape('text', {
          attrs: {
            text: line ? truncate(line.text, 34) : '',
            x: -NODE_WIDTH / 2 + 14,
            y: summaryLineY(layout, index),
            textAlign: 'left',
            textBaseline: 'middle',
            fill: summaryLineColor(line, colors),
            fontSize: line?.emphasis ? 12 : 10,
            fontWeight: line?.emphasis ? 600 : 400,
            opacity: line ? 1 : 0
          },
          name: `workflow-node-summary-${index}`,
          capture: false
        })
      }

      group.addShape('line', {
        attrs: {
          x1: -NODE_WIDTH / 2 + 12,
          y1: summaryDividerY(layout),
          x2: NODE_WIDTH / 2 - 12,
          y2: summaryDividerY(layout),
          stroke: colors.border,
          lineWidth: 1,
          opacity: layout.summaryLines.length ? 1 : 0
        },
        name: 'workflow-node-summary-divider',
        capture: false
      })

      layout.inputPorts.forEach((port, index) => {
        const y = portY(layout, index)
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

      layout.outputPorts.forEach((port, index) => {
        const y = portY(layout, index)
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
      const layout = nodeLayout(cfg)
      return [
        ...layout.inputPorts.map((_, index) => [0, normalizedPortY(layout, index)]),
        ...layout.outputPorts.map((_, index) => [1, normalizedPortY(layout, index)])
      ]
    },

    update(cfg, item) {
      updateNodeShapes(item?.getContainer?.(), { ...item?.getModel?.(), ...cfg })
    }
  }, 'single-node')

  return NODE_TYPE
}

function nodeLayout(cfg = {}) {
  const inputPorts = cfg.inputPorts || []
  const outputPorts = cfg.outputPorts || []
  const summaryLines = parameterSummaryLines(cfg.parameterSummaries || [])
  const summaryHeight = summaryLines.length ? 12 + summaryLines.length * SUMMARY_LINE_HEIGHT + 8 : 0
  const portRows = Math.max(inputPorts.length, outputPorts.length, 1)
  return {
    inputPorts,
    outputPorts,
    summaryLines,
    summaryHeight,
    height: HEADER_HEIGHT + summaryHeight + 18 + portRows * PORT_ROW_HEIGHT
  }
}

function portY(layout, index) {
  return -layout.height / 2 + HEADER_HEIGHT + layout.summaryHeight + 18 + index * PORT_ROW_HEIGHT
}

function normalizedPortY(layout, index) {
  return (portY(layout, index) + layout.height / 2) / layout.height
}

function summaryLineY(layout, index) {
  return -layout.height / 2 + HEADER_HEIGHT + 12 + index * SUMMARY_LINE_HEIGHT
}

function summaryDividerY(layout) {
  return -layout.height / 2 + HEADER_HEIGHT + layout.summaryHeight
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

function updateNodeShapes(group, cfg) {
  if (!group) return
  const layout = nodeLayout(cfg)
  const colors = canvasThemeColors()
  group.find(shape => shape.get('name') === 'workflow-node-card')?.attr({
    y: -layout.height / 2,
    height: layout.height
  })
  group.find(shape => shape.get('name') === 'workflow-node-title')?.attr({
    text: cfg.displayName || cfg.label || cfg.operator,
    y: -layout.height / 2 + HEADER_HEIGHT / 2
  })
  group.find(shape => shape.get('name') === 'workflow-node-divider')?.attr({
    y1: -layout.height / 2 + HEADER_HEIGHT,
    y2: -layout.height / 2 + HEADER_HEIGHT
  })

  for (let index = 0; index < MAX_SUMMARY_LINES; index += 1) {
    const line = layout.summaryLines[index]
    group.find(shape => shape.get('name') === `workflow-node-summary-${index}`)?.attr({
      text: line ? truncate(line.text, 34) : '',
      y: summaryLineY(layout, index),
      fill: summaryLineColor(line, colors),
      fontSize: line?.emphasis ? 12 : 10,
      fontWeight: line?.emphasis ? 600 : 400,
      opacity: line ? 1 : 0
    })
  }
  group.find(shape => shape.get('name') === 'workflow-node-summary-divider')?.attr({
    y1: summaryDividerY(layout),
    y2: summaryDividerY(layout),
    opacity: layout.summaryLines.length ? 1 : 0
  })

  layout.inputPorts.forEach((port, index) => updatePortShapes(group, port, 'input', portY(layout, index)))
  layout.outputPorts.forEach((port, index) => updatePortShapes(group, port, 'output', portY(layout, index)))
}

function updatePortShapes(group, port, portType, y) {
  group.find(shape => shape.get('name') === `${portType}-port-${port.name}`)?.attr({ y })
  group.find(shape => shape.get('name') === `port-label-${port.name}`)?.attr({ y })
}

function parameterSummaryLines(summaries) {
  const selected = summaries.slice(0, MAX_SUMMARY_ITEMS)
  const lines = selected.flatMap(summary => {
    if (summary.kind === 'resource' && summary.configured) {
      const resourcePath = summary.path || summary.resourceName || summary.value
      return [
        {
          text: resourcePath,
          emphasis: true,
          configured: true
        },
        ...(summary.engineName ? [{ text: summary.engineName, configured: true }] : [])
      ]
    }
    return [{
      text: `${summary.label}: ${summary.value}`,
      configured: summary.configured
    }]
  })
  if (summaries.length > MAX_SUMMARY_ITEMS) {
    lines.push({ text: `+${summaries.length - MAX_SUMMARY_ITEMS}`, configured: true })
  }
  return lines.slice(0, MAX_SUMMARY_LINES)
}

function summaryLineColor(line, colors) {
  if (line?.configured === false) return colors.warning
  return line?.emphasis ? colors.textPrimary : colors.textSecondary
}

function truncate(value, length) {
  const text = String(value || '')
  return text.length > length ? `${text.slice(0, length - 1)}…` : text
}

function canvasThemeColors() {
  const style = getComputedStyle(document.documentElement)
  return {
    background: style.getPropertyValue('--addp-bg-primary').trim(),
    border: style.getPropertyValue('--addp-border-color').trim(),
    textPrimary: style.getPropertyValue('--addp-text-primary').trim(),
    textSecondary: style.getPropertyValue('--addp-text-secondary').trim(),
    primary: style.getPropertyValue('--el-color-primary').trim(),
    success: style.getPropertyValue('--el-color-success').trim(),
    warning: style.getPropertyValue('--el-color-warning').trim()
  }
}
