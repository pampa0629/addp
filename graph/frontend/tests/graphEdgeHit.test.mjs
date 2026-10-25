import { expect, it, vi } from 'vitest'
import { nearestGraphEdge } from '../src/utils/graphEdgeHit.js'

function edge(pointAt, options = {}) {
  const shape = {
    get: key => key === 'capture' ? options.capture !== false : options.visible !== false,
    getTotalMatrix: () => options.matrix || null,
    isInShape: vi.fn(() => options.hit !== false),
    getPoint: pointAt
  }
  return { getContainer: () => ({ get: () => options.groupVisible !== false }), getKeyShape: () => shape }
}

it('uses the native local hit test and actual viewport transform before comparing strokes', () => {
  const transformed = edge(t => ({ x: t * 10, y: 0 }), { matrix: [2,0,0,0,2,0,100,50,1] })
  const later = edge(t => ({ x:100+t*20, y:52 }))
  expect(nearestGraphEdge([transformed,later], {x:110,y:50}, later)).toBe(transformed)
  expect(transformed.getKeyShape().isInShape).toHaveBeenCalledWith(5,0)
})

it('ignores hidden, uncapturable and natively rejected relationships', () => {
  const options = [{visible:false},{groupVisible:false},{capture:false},{hit:false},{matrix:[0,0,0,0,0,0,0,0,1]}]
  const excluded = options.map(value => edge(t=>({x:t,y:0}),value))
  const visible = edge(t=>({x:t,y:1}))
  expect(nearestGraphEdge([...excluded,visible],{x:.5,y:0},excluded[0])).toBe(visible)
  expect(nearestGraphEdge(excluded,{x:.5,y:0},excluded[0])).toBeNull()
})

it('keeps native drawing order at an exact intersection without conflating identities', () => {
  const first=edge(t=>({x:t*10,y:0})), second=edge(t=>({x:5,y:t*10-5}))
  expect(nearestGraphEdge([first,second],{x:5,y:0},first)).toBe(first)
  expect(nearestGraphEdge([first,second],{x:5,y:0},second)).toBe(second)
})

it('handles self-loop paths and endpoint minima', () => {
  const loop=edge(t=>({x:Math.cos(t*Math.PI*2)*10,y:Math.sin(t*Math.PI*2)*10}))
  const line=edge(t=>({x:t*20-10,y:11}))
  expect(nearestGraphEdge([loop,line],{x:0,y:10},line)).toBe(loop)
  expect(nearestGraphEdge([loop,line],{x:10,y:0},line)).toBe(loop)
})

it('does not select from invalid mouse coordinates or invalid path geometry', () => {
  const valid=edge(t=>({x:t,y:0}))
  expect(nearestGraphEdge([valid],{x:NaN,y:0},valid)).toBeNull()
  expect(nearestGraphEdge([edge(()=>null)],{x:1,y:0},null)).toBeNull()
  expect(nearestGraphEdge([edge(()=>({x:Infinity,y:0}))],{x:1,y:0},null)).toBeNull()
})

it('uses real GCanvas quadratic path geometry for the recorded Hosted coordinate', async () => {
  const { Shape } = await import('@antv/g-canvas')
  const Path = Shape.Path
  const shape = new Path({ attrs: { path: [['M',495.02350382842013,484.8057522096755],['Q',535.9033649001326,459.46953851326833,540.1623105860343,411.563921438054]],stroke:'#0f766e',lineWidth:2,lineAppendWidth:2 } })
  const actual = { getContainer: () => ({get:()=>true}), getKeyShape:()=>shape }
  expect(shape.isInShape(536,430)).toBe(true)
  expect(nearestGraphEdge([actual],{x:536,y:430},actual)).toBe(actual)
})
