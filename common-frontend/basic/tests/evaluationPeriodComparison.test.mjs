import test from 'node:test'
import assert from 'node:assert/strict'
import { compareEvaluationPeriods } from '../src/utils/evaluationPeriodComparison.mjs'
const input = (stepSeconds = 15, gridStart = 0) => ({ start: 0, end: 600000, periodSeconds: 300, stepSeconds, gridStart, points: Array.from({ length: Math.floor((600000 - gridStart) / (stepSeconds * 1000)) + 1 }, (_, i) => ({ at: gridStart + i * stepSeconds * 1000, value: gridStart + i * stepSeconds * 1000 <= 300000 ? 0 : 1.125 })) })
test('adjacent periods count the shared boundary once and retain unrounded means and zero baseline', () => {
  const result = compareEvaluationPeriods(input())
  assert.equal(result.complete, true)
  assert.deepEqual([result.previous.valid, result.previous.total, result.current.valid, result.current.total], [20, 20, 20, 20])
  assert.equal(result.previous.mean, 0); assert.equal(result.current.mean, 1.125); assert.equal(result.delta, 1.125)
  const uneven = compareEvaluationPeriods(input(45))
  assert.equal(uneven.previous.total, 6); assert.equal(uneven.current.total, 7)
})
test('a gap in either period suppresses both means, and no missing point becomes zero', () => {
  for (const index of [1, 20, 21, 40]) {
    const value = input(); value.points[index].value = null
    const result = compareEvaluationPeriods(value)
    assert.equal(result.complete, false); assert.equal(result.delta, null)
    assert.equal(result.previous.mean, null); assert.equal(result.current.mean, null)
    assert.equal(result.previous.valid + result.current.valid, 39)
  }
})
test('clipped and phase-shifted startup grids count unavailable history instead of comparing partial periods', () => {
  for (const first of [1000, 15000, 301000, 600000]) {
    const result = compareEvaluationPeriods(input(15, first))
    assert.equal(result.complete, false); assert.equal(result.delta, null)
    assert.equal(result.previous.total, 20); assert.equal(result.current.total, 20)
  }
})
test('zero evaluations in a period cannot produce a mean', () => {
  const result = compareEvaluationPeriods(input(600))
  assert.equal(result.complete, false); assert.equal(result.previous.total, 0)
})
test('invalid bounds, evaluation grids, values and query-size overflow fail closed', () => {
  for (const mutate of [x => x.stepSeconds = 0, x => x.start = NaN, x => x.gridStart = -1, x => x.periodSeconds = 100, x => x.points.pop(), x => x.points[1].at = 0, x => x.points[1].value = NaN, x => x.points[1].value = undefined, x => x.points.forEach(p => p.value = Number.MAX_VALUE), x => x.stepSeconds = 0.001]) {
    const value = input(); mutate(value)
    assert.throws(() => compareEvaluationPeriods(value))
  }
})
