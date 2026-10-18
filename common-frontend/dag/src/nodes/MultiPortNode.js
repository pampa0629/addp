/**
 * 只读 DAG 查看器的多输出端口节点定义
 */
import G6 from '@antv/g6'

/**
 * 注册多端口节点类型
 */
export function registerMultiPortNode() {
  // 幂等性检测
  if (G6.getNodeType && G6.getNodeType('workflow-node')) {
    return
  }

  G6.registerNode('workflow-node', {
    draw(cfg, group) {
      const outputPorts = cfg.outputPorts || [{ name: 'default', description: '默认输出' }]
      const nodeWidth = 140
      const nodeHeight = Math.max(60, 40 + outputPorts.length * 20)

      // 主矩形
      const rect = group.addShape('rect', {
        attrs: {
          x: -nodeWidth / 2,
          y: -nodeHeight / 2,
          width: nodeWidth,
          height: nodeHeight,
          radius: 4,
          ...cfg.style
        },
        name: 'main-rect'
      })

      // 节点标签
      group.addShape('text', {
        attrs: {
          text: cfg.label,
          x: 0,
          y: -nodeHeight / 2 + 20,
          textAlign: 'center',
          textBaseline: 'middle',
          ...cfg.labelCfg.style,
          fontSize: 13,
          fontWeight: 500
        },
        name: 'node-label'
      })

      // 输入端口（顶部中心）
      group.addShape('circle', {
        attrs: {
          x: 0,
          y: -nodeHeight / 2,
          r: 6,
          fill: cfg.portStyle.fill,
          stroke: cfg.portStyle.input,
          lineWidth: 2,
          cursor: 'pointer'
        },
        name: 'input-port',
        portType: 'input',
        portName: 'input'
      })

      // 输出端口（底部，根据数量排列）
      outputPorts.forEach((port, index) => {
        const portCount = outputPorts.length
        const spacing = nodeWidth / (portCount + 1)
        const xOffset = -nodeWidth / 2 + spacing * (index + 1)

        // 端口圆点
        group.addShape('circle', {
          attrs: {
            x: xOffset,
            y: nodeHeight / 2,
            r: 7,
            fill: cfg.portStyle.fill,
            stroke: port.is_default ? cfg.portStyle.defaultOutput : cfg.portStyle.output,
            lineWidth: 2,
            cursor: 'pointer'
          },
          name: `output-port-${port.name}`,
          portType: 'output',
          portName: port.name,
          portDescription: port.description
        })

        // 端口标签（仅非 default 端口显示）
        if (!port.is_default && portCount > 1) {
          group.addShape('text', {
            attrs: {
              text: port.name,
              x: xOffset,
              y: nodeHeight / 2 + 16,
              textAlign: 'center',
              textBaseline: 'top',
              fill: cfg.portStyle.label,
              fontSize: 10
            },
            name: `port-label-${port.name}`
          })
        }
      })

      return rect
    },

    update(cfg, node) {
      const group = node.getContainer()
      const find = name => group.find(shape => shape.get('name') === name)
      find('main-rect').attr(cfg.style)
      find('node-label').attr({ ...cfg.labelCfg.style, text: cfg.label })
      find('input-port').attr({ fill: cfg.portStyle.fill, stroke: cfg.portStyle.input })
      const outputPorts = cfg.outputPorts || [{ name: 'default' }]
      for (const port of outputPorts) {
        find(`output-port-${port.name}`)?.attr({ fill: cfg.portStyle.fill, stroke: port.is_default ? cfg.portStyle.defaultOutput : cfg.portStyle.output })
        find(`port-label-${port.name}`)?.attr({ fill: cfg.portStyle.label })
      }
    },

    getAnchorPoints() {
      return [
        [0.5, 0],   // 顶部输入
        [0.5, 1]    // 底部输出
      ]
    }
  }, 'single-node')
}
