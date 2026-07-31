const TAU = Math.PI * 2

export function placeIncrementalNodes(data, existingPositions, anchorId) {
  const anchor = existingPositions.get(anchorId)
  if (!anchor) return false

  const nodesByID = new Map(data.nodes.map(node => [node.id, node]))
  data.nodes.forEach(node => {
    const position = existingPositions.get(node.id)
    if (!position) return
    node.x = position.x
    node.y = position.y
  })

  const addedNodes = data.nodes.filter(node => !existingPositions.has(node.id))
  if (addedNodes.length === 0) return true

  const addedIDs = new Set(addedNodes.map(node => node.id))
  const adjacency = buildAdjacency(data.edges, nodesByID)
  const placements = discoverPlacements(anchorId, addedIDs, adjacency)
  const rootBranches = [...new Set([...placements.values()].filter(item => item.hop === 1).map(item => item.branch))].sort()
  const branchIndex = new Map(rootBranches.map((id, index) => [id, index]))
  const siblingGroups = groupByParent(placements)
  const ideals = new Map()

  addedNodes
    .slice()
    .sort((a, b) => (placements.get(a.id)?.hop || Infinity) - (placements.get(b.id)?.hop || Infinity) || a.id.localeCompare(b.id))
    .forEach((node, fallbackIndex) => {
      const placement = placements.get(node.id)
      if (!placement) {
        const angle = (fallbackIndex / Math.max(addedNodes.length, 1)) * TAU
        const radius = 180 + Math.floor(fallbackIndex / 16) * 100
        node.x = anchor.x + Math.cos(angle) * radius
        node.y = anchor.y + Math.sin(angle) * radius
        ideals.set(node.id, { x: node.x, y: node.y })
        return
      }

      const siblings = siblingGroups.get(placement.parent) || [node.id]
      const siblingIndex = siblings.indexOf(node.id)
      const branch = branchIndex.get(placement.branch) ?? 0
      const branchCount = Math.max(rootBranches.length, 1)
      const baseAngle = (branch / branchCount) * TAU
      const localOffset = (siblingIndex - (siblings.length - 1) / 2) * Math.min(0.24, 0.8 / Math.max(siblings.length, 1))
      const angle = baseAngle + localOffset
      const radius = 130 + (placement.hop - 1) * 115
      node.x = anchor.x + Math.cos(angle) * radius
      node.y = anchor.y + Math.sin(angle) * radius
      ideals.set(node.id, { x: node.x, y: node.y })
    })

  relaxAddedNodes(addedNodes, existingPositions, ideals)
  return true
}

function buildAdjacency(edges, nodesByID) {
  const adjacency = new Map([...nodesByID.keys()].map(id => [id, []]))
  edges.forEach(edge => {
    const source = typeof edge.source === 'object' ? edge.source.id : edge.source
    const target = typeof edge.target === 'object' ? edge.target.id : edge.target
    if (!nodesByID.has(source) || !nodesByID.has(target)) return
    adjacency.get(source).push(target)
    adjacency.get(target).push(source)
  })
  adjacency.forEach(neighbors => neighbors.sort())
  return adjacency
}

function discoverPlacements(anchorId, addedIDs, adjacency) {
  const placements = new Map()
  const visited = new Set([anchorId])
  const queue = [{ id: anchorId, hop: 0, branch: '' }]
  while (queue.length > 0) {
    const current = queue.shift()
    for (const neighbor of adjacency.get(current.id) || []) {
      if (visited.has(neighbor)) continue
      visited.add(neighbor)
      const branch = current.hop === 0 ? neighbor : current.branch
      if (addedIDs.has(neighbor)) {
        placements.set(neighbor, {
          parent: current.id,
          hop: current.hop + 1,
          branch,
        })
      }
      queue.push({ id: neighbor, hop: current.hop + 1, branch })
    }
  }
  return placements
}

function groupByParent(placements) {
  const groups = new Map()
  placements.forEach((placement, id) => {
    if (!groups.has(placement.parent)) groups.set(placement.parent, [])
    groups.get(placement.parent).push(id)
  })
  groups.forEach(ids => ids.sort())
  return groups
}

function relaxAddedNodes(nodes, fixedPositions, ideals) {
  const minDistance = 76
  const fixed = [...fixedPositions.values()]
  for (let iteration = 0; iteration < 12; iteration += 1) {
    nodes.forEach(node => {
      const ideal = ideals.get(node.id)
      node.x += (ideal.x - node.x) * 0.12
      node.y += (ideal.y - node.y) * 0.12
    })
    for (let left = 0; left < nodes.length; left += 1) {
      for (let right = left + 1; right < nodes.length; right += 1) {
        separate(nodes[left], nodes[right], minDistance, true)
      }
      fixed.forEach(position => separate(nodes[left], position, minDistance, false))
    }
  }
}

function separate(movable, other, minDistance, moveBoth) {
  let dx = movable.x - other.x
  let dy = movable.y - other.y
  let distance = Math.hypot(dx, dy)
  if (distance >= minDistance) return
  if (distance < 0.001) {
    dx = 1
    dy = 0
    distance = 1
  }
  const displacement = (minDistance - distance) / (moveBoth ? 2 : 1)
  const offsetX = (dx / distance) * displacement
  const offsetY = (dy / distance) * displacement
  movable.x += offsetX
  movable.y += offsetY
  if (moveBoth) {
    other.x -= offsetX
    other.y -= offsetY
  }
}
