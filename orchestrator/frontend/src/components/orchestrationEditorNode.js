import G6 from '@antv/g6'

export const ORCHESTRATION_NODE_TYPE = 'orchestration-task-node'

const NODE_WIDTH = 280
const HEADER_HEIGHT = 62
const ROW_HEIGHT = 24
const BODY_PADDING = 14

export function registerOrchestrationEditorNode() {
  if (G6.getNodeType?.(ORCHESTRATION_NODE_TYPE)) return ORCHESTRATION_NODE_TYPE

  G6.registerNode(ORCHESTRATION_NODE_TYPE, {
    draw(cfg, group) {
      const inputs = cfg.inputPorts || []
      const outputs = cfg.outputPorts || []
      const rows = Math.max(inputs.length, outputs.length, 1)
      const height = nodeHeight(rows)
      const colors = themeColors()
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
        name: 'orchestration-node-card',
        draggable: true
      })

      group.addShape('line', {
        attrs: {
          x1: -NODE_WIDTH / 2,
          y1: -height / 2,
          x2: -NODE_WIDTH / 2,
          y2: -height / 2 + HEADER_HEIGHT,
          stroke: cfg.providerColor || colors.primary,
          lineWidth: 4
        },
        name: 'orchestration-node-accent',
        capture: false
      })
      group.addShape('text', {
        attrs: {
          text: nodeTitle(cfg.label || cfg.name || cfg.id),
          x: -NODE_WIDTH / 2 + 14,
          y: -height / 2 + HEADER_HEIGHT / 2,
          textAlign: 'left',
          textBaseline: 'middle',
          fill: colors.textPrimary,
          fontSize: 14,
          lineHeight: 20,
          fontWeight: 600
        },
        name: 'orchestration-node-title',
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
        name: 'orchestration-node-divider',
        capture: false
      })

      drawControlPort(group, -NODE_WIDTH / 2, controlY(height), 'input', colors)
      drawControlPort(group, NODE_WIDTH / 2, controlY(height), 'output', colors)
      inputs.forEach((port, index) => drawParameterPort(group, {
        x: -NODE_WIDTH / 2,
        y: portY(height, index),
        port,
        direction: 'input',
        colors
      }))
      outputs.forEach((port, index) => drawParameterPort(group, {
        x: NODE_WIDTH / 2,
        y: portY(height, index),
        port,
        direction: 'output',
        colors
      }))
      return rect
    },

    getAnchorPoints(cfg) {
      const inputs = cfg.inputPorts || []
      const outputs = cfg.outputPorts || []
      const height = nodeHeight(Math.max(inputs.length, outputs.length, 1))
      return [
        [0, normalizedY(height, controlY(height))],
        ...inputs.map((_, index) => [0, normalizedY(height, portY(height, index))]),
        [1, normalizedY(height, controlY(height))],
        ...outputs.map((_, index) => [1, normalizedY(height, portY(height, index))])
      ]
    },

    update(cfg, item) {
      const group = item?.getContainer?.()
      group?.find(shape => shape.get('name') === 'orchestration-node-title')?.attr({
        text: nodeTitle(cfg.label || cfg.name || cfg.id)
      })
      group?.find(shape => shape.get('name') === 'orchestration-node-accent')?.attr({
        stroke: cfg.providerColor || themeColors().primary
      })
    },

    setState(name, selected, item) {
      if (name !== 'selected') return
      const colors = themeColors()
      const card = item?.getContainer?.()?.find(shape => shape.get('name') === 'orchestration-node-card')
      card?.attr({
        stroke: selected ? colors.danger : colors.border,
        lineWidth: selected ? 3 : 1.5,
        shadowColor: selected ? colors.danger : 'transparent',
        shadowBlur: selected ? 6 : 0
      })
    }
  }, 'single-node')

  return ORCHESTRATION_NODE_TYPE
}

export function orchestrationPort(event, direction) {
  const shape = event?.shape
  if (shape?.cfg?.portDirection !== direction) return null
  return {
    kind: shape.cfg.portKind,
    name: shape.cfg.portName,
    path: shape.cfg.portPath,
    type: shape.cfg.portDataType
  }
}

export function orchestrationAnchor(model, direction, kind, name = '') {
  const inputs = model?.inputPorts || []
  const outputs = model?.outputPorts || []
  if (direction === 'input') {
    if (kind === 'control') return 0
    return 1 + inputs.findIndex(port => port.name === name)
  }
  if (kind === 'control') return 1 + inputs.length
  return 2 + inputs.length + outputs.findIndex(port => port.name === name)
}

function drawControlPort(group, x, y, direction, colors) {
  group.addShape('path', {
    attrs: {
      path: [['M', x, y - 6], ['L', x + 6, y], ['L', x, y + 6], ['L', x - 6, y], ['Z']],
      fill: colors.background,
      stroke: colors.warning,
      lineWidth: 2,
      cursor: 'crosshair'
    },
    name: `control-${direction}-port`,
    draggable: true,
    portKind: 'control',
    portType: direction,
    portDirection: direction,
    portName: 'control'
  })
}

function drawParameterPort(group, { x, y, port, direction, colors }) {
  group.addShape('circle', {
    attrs: {
      x,
      y,
      r: 6,
      fill: colors.background,
      stroke: direction === 'input' ? colors.success : colors.primary,
      lineWidth: 2,
      cursor: 'crosshair'
    },
    name: `${direction}-port-${port.name}`,
    draggable: true,
    portKind: 'parameter',
    portType: direction,
    portDirection: direction,
    portName: port.name,
    portPath: port.path,
    portDataType: port.type
  })
  group.addShape('text', {
    attrs: {
      text: portLabel(port.label || port.name),
      x: x + (direction === 'input' ? 13 : -13),
      y,
      textAlign: direction === 'input' ? 'left' : 'right',
      textBaseline: 'middle',
      fill: colors.textSecondary,
      fontSize: 11
    },
    name: `${direction}-label-${port.name}`,
    capture: false
  })
}

export function orchestrationNodeSize(model) {
  return [NODE_WIDTH, nodeHeight(Math.max(model.inputPorts?.length || 0, model.outputPorts?.length || 0, 1))]
}

function nodeTitle(value) {
  const lines = ['']
  for (const character of String(value || '')) {
    const index = lines.length - 1
    if (G6.Util.getTextSize(lines[index] + character, 14)[0] > NODE_WIDTH - 32) {
      if (lines.length === 2) {
        lines[index] = lines[index].slice(0, -1) + '…'
        break
      }
      lines.push(character)
    } else {
      lines[index] += character
    }
  }
  return lines.join('\n')
}

function nodeHeight(rows) {
  return HEADER_HEIGHT + BODY_PADDING * 2 + rows * ROW_HEIGHT
}

function controlY(height) {
  return -height / 2 + HEADER_HEIGHT / 2
}

function portY(height, index) {
  return -height / 2 + HEADER_HEIGHT + BODY_PADDING + ROW_HEIGHT / 2 + index * ROW_HEIGHT
}

function normalizedY(height, y) {
  return (y + height / 2) / height
}

function portLabel(value) {
  const text = String(value || '')
  let label = text
  while (label && G6.Util.getTextSize(label + (label === text ? '' : '…'), 11)[0] > NODE_WIDTH / 2 - 24) {
    label = label.slice(0, -1)
  }
  return label === text ? label : `${label}…`
}

function themeColors() {
  const style = getComputedStyle(document.documentElement)
  const color = name => style.getPropertyValue(name).trim()
  return {
    background: color('--addp-bg-primary'),
    border: color('--addp-border-color'),
    textPrimary: color('--addp-text-primary'),
    textSecondary: color('--addp-text-secondary'),
    primary: color('--el-color-primary'),
    success: color('--el-color-success'),
    warning: color('--el-color-warning'),
    danger: color('--el-color-danger')
  }
}

// Execution adornments live only in render shapes, never in the editable node model.
export function renderOrchestrationNodeExecution(item, state, label) {
  const group = item?.getContainer?.()
  if (!group) return
  for (const name of ['execution-badge', 'execution-label']) {
    group.find(shape => shape.get('name') === name)?.remove()
  }
  const existingPulse = group.find(shape => shape.get('name') === 'execution-pulse')
  if (state?.status !== 'running' && existingPulse) {
    existingPulse.stopAnimate()
    existingPulse.remove()
  }
  if (!state) return
  const colors = themeColors()
  const color = ({ running: colors.warning, success: colors.success, failed: colors.danger,
    timeout: colors.danger })[state.status] || colors.textSecondary
  const height = orchestrationNodeSize(item.getModel())[1]
  if (state.status === 'running') {
    const attrs = { x: -NODE_WIDTH / 2 - 5, y: -height / 2 - 5,
      width: NODE_WIDTH + 10, height: height + 10, radius: 10,
      stroke: color, lineWidth: 4 }
    if (existingPulse) {
      // Keep the animation phase when polling updates the same running step.
      existingPulse.attr(attrs)
    } else {
      const pulse = group.addShape('rect', {
        attrs: { ...attrs, opacity: 1 }, name: 'execution-pulse', capture: false
      })
      if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        pulse.animate(ratio => ({ opacity: 0.65 + 0.35 * Math.cos(ratio * Math.PI * 2) }),
          { duration: 1600, repeat: true })
      }
    }
  }
  const width = Math.min(NODE_WIDTH, G6.Util.getTextSize(label, 12)[0] + 20)
  group.addShape('rect', {
    attrs: { x: NODE_WIDTH / 2 - width, y: -height / 2 - 20, width, height: 18,
      fill: colors.background, stroke: color, radius: 4 },
    name: 'execution-badge', capture: false
  })
  group.addShape('text', {
    attrs: { text: label, x: NODE_WIDTH / 2 - width / 2, y: -height / 2 - 11,
      fill: color, textAlign: 'center', textBaseline: 'middle', fontSize: 12, fontWeight: 600 },
    name: 'execution-label', capture: false
  })
}
