import { expect,test } from '@playwright/test'
import { engineBackend,engineID,engineOwner } from './engine-resources.fixture'
import { trackResourceChart,resourceChartState,resourceChartProps,dragResourceChartZoom,hoverResourceChartPoint,resourceChartTooltip } from './resource-chart.fixture'

const sourceStates = [
  ['not-connected', '未部署采集来源', 'Collection source not deployed'],
  ['source-unavailable', '采集来源不可用', 'Collection source unavailable'],
  ['no-sample', '暂无采样', 'No samples yet'],
  ['scrape-failed', '抓取失败', 'Scrape failed'],
  ['snapshot-unavailable', '未取得有效数据库快照', 'No valid database snapshot'],
  ['stale', '样本已过期', 'Stale samples']
]
for (const locale of ['zh-cn', 'en']) for (const detail of [false, true]) test(`Redis ${detail ? 'detail' : 'list'} separates source states and restores current values (${locale})`, async ({ page }) => {
  const en = locale === 'en', state = await engineBackend(page, { locale, owner: { ...engineOwner, engine_type: 'redis', display_name: 'Business Redis' } })
  await page.goto(`/engine-resources?engine_type=redis&refresh=off${detail ? '&deployment_id=' + engineID : ''}`)
  const content = detail ? page.getByTestId('engine-value-redisAllocated') : page.getByTestId('engine-list')
  const refresh = async () => {
    const before = state.calls.length
    await page.getByRole('button', { name: en ? 'Refresh' : '刷新', exact: true }).click()
    await expect.poll(() => state.calls.length).toBeGreaterThan(before)
    await expect(page.locator('.engine-resources')).toHaveAttribute('aria-busy', 'false')
  }
  await expect(content).toContainText('1.00 GiB')
  for (const [index, [mode, zh, english]] of sourceStates.entries()) {
    state.setMode(mode); await refresh()
    await expect(page.getByText(en ? english : zh, { exact: true })).toBeVisible()
    await expect(page.getByTestId('engine-error')).toHaveCount(0)
    if (detail) {
      for (const field of await page.getByTestId('engine-metrics').locator('strong').all()) await expect(field).toHaveText('—')
      const props = await resourceChartProps(page, page.getByTestId('engine-trend').locator('.chart-renderer'))
      expect(props.rows.at(-1).value).toBeNull()
      if (['scrape-failed', 'snapshot-unavailable', 'stale'].includes(mode)) expect(props.rows.some(row => row.value !== null)).toBe(true)
      else expect(props.rows.every(row => row.value === null)).toBe(true)
    } else await expect(content).not.toContainText('GiB')
    state.setMode('valid'); state.setCapacity((index + 2) * 1024 ** 3); await refresh()
    await expect(content).toContainText(`${index + 2}.00 GiB`)
    await expect(page.getByText(en ? 'Collecting' : '正在采集', { exact: true })).toBeVisible()
  }
  state.owner.enabled = false; state.owner.version++; await refresh()
  await expect(page.getByText(en ? 'Disabled or invalid host association' : '部署停用或主机关联无效', { exact: true })).toBeVisible()
  await expect(content).not.toContainText('GiB')
  state.owner.enabled = true; state.owner.version++; state.setCapacity(8 * 1024 ** 3); await refresh()
  await expect(content).toContainText('8.00 GiB')
  expect(state.queries.every(query => query.engine_type === 'redis')).toBe(true)
  if (!detail) expect(state.calls.some(path => path.endsWith('_trends') || path.endsWith('_observations'))).toBe(false)
})

for (const locale of ['zh-cn', 'en']) test(`Redis automatic recovery pauses while hidden and stops after permission denial (${locale})`, async ({ page }) => {
  const en = locale === 'en', state = await engineBackend(page, { locale, owner: { ...engineOwner, engine_type: 'redis', display_name: 'Infra Redis' } })
  await page.clock.install()
  await page.goto(`/engine-resources?engine_type=redis&deployment_id=${engineID}`)
  const field = page.getByTestId('engine-value-redisAllocated')
  const reads = () => state.calls.filter(path => path.endsWith('_observations')).length
  const visibility = hidden => page.evaluate(hidden => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: hidden })
    document.dispatchEvent(new Event('visibilitychange'))
  }, hidden)
  await expect(field).toHaveText('1.00 GiB')
  await expect(page.locator('.engine-resources')).toHaveAttribute('aria-busy', 'false')
  state.setMode('source-unavailable')
  await page.clock.fastForward(15001)
  await expect(page.getByText(en ? 'Collection source unavailable' : '采集来源不可用', { exact: true })).toBeVisible()
  await expect(field).toHaveText('—')
  await expect(page.locator('.engine-resources')).toHaveAttribute('aria-busy', 'false')
  expect(reads()).toBe(2)
  await visibility(true)
  await expect(page.getByTestId('engine-refresh-status')).toHaveText(en ? 'Page hidden; auto refresh paused' : '页面隐藏，自动刷新已暂停')
  state.setMode('valid'); state.setCapacity(2 * 1024 ** 3)
  const hiddenCalls = state.calls.length
  await page.clock.fastForward(60001)
  expect(state.calls).toHaveLength(hiddenCalls)
  await visibility(false)
  await expect(field).toHaveText('2.00 GiB')
  await expect(page.locator('.engine-resources')).toHaveAttribute('aria-busy', 'false')
  expect(reads()).toBe(3)
  state.setMode('denied')
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('engine-error')).toBeVisible()
  await expect(field).toHaveText('—')
  await expect(page.getByTestId('engine-trend').locator('canvas')).toHaveCount(0)
  await expect(page.getByTestId('engine-refresh-status')).toHaveText(en ? 'Auto refresh stopped; reload to retry' : '自动刷新已停止，请重新加载')
  const deniedCalls = state.calls.length
  state.setMode('valid')
  await page.clock.fastForward(60001); await visibility(true); await visibility(false)
  expect(state.calls).toHaveLength(deniedCalls)
  await page.getByRole('button', { name: en ? 'Refresh' : '刷新', exact: true }).click()
  await expect(field).toHaveText('2.00 GiB')
  await expect(page.getByTestId('engine-refresh-status')).toHaveText(en ? 'Auto refresh enabled' : '自动刷新已开启')
})

for(const locale of ['zh-cn','en'])test(`Redis groups, status tips and direct owner-typed navigation (${locale})`,async({page})=>{
 const state=await engineBackend(page,{locale,owner:{...engineOwner,engine_type:'redis',display_name:'Business Redis'}})
 const requests=[];page.on('request',r=>{if(r.url().includes('engine_resource_observations'))requests.push(new URL(r.url()).searchParams.get('metrics').split(','))})
 await page.goto(`/engine-resources?deployment_id=${engineID}&refresh=off`)
 await expect(page).toHaveURL(/engine_type=redis/)
 await expect(page.getByTestId('engine-metrics').locator('article')).toHaveCount(10)
 await expect(page.getByTestId('engine-value-redisAllocated')).toHaveText('1.00 GiB')
 await expect(page.getByTestId('engine-trend').locator('canvas')).toBeVisible()
 await page.getByRole('tab',{name:locale==='en'?'Persistence status':'持久化状态',exact:true}).click()
 await expect(page.getByTestId('engine-metrics').locator('article')).toHaveCount(5)
 await expect(page.getByTestId('engine-value-redisAOF')).toHaveText(locale==='en'?'Disabled':'已关闭')
 await page.getByTestId('engine-metric-redisRDB').getByRole('button',{name:locale==='en'?'Result of the last save; success does not prove scheduled saves or a recoverable backup.':'最近保存命令的状态；成功不能证明已启用定时保存或已有可恢复备份。',exact:true}).focus()
 await expect(page.getByRole('tooltip')).toBeVisible()
 await page.getByRole('tab',{name:locale==='en'?'Throughput and hits':'吞吐与命中',exact:true}).click()
 await expect(page.getByTestId('engine-metrics').locator('article')).toHaveCount(7)
 await expect(page).toHaveURL(/metric=redis.commands.per_second/)
 expect(requests.map(keys=>keys.length)).toEqual([10,5,7])
 expect(requests.flat().every(key=>key.startsWith('redis.'))).toBe(true)
 state.setMode('denied');await page.getByRole('button',{name:locale==='en'?'Refresh':'刷新',exact:true}).click()
 await expect(page.getByTestId('engine-error')).toBeVisible();await expect(page.getByTestId('engine-value-redisCommands')).toHaveText('—')
})

test('Redis missing ratios stay blank and type filter applies before paging',async({page})=>{
 const state=await engineBackend(page,{owner:{...engineOwner,engine_type:'redis',display_name:'Infra Redis'},mode:'no-limit'})
 await page.goto('/engine-resources?engine_type=redis&refresh=off')
 await expect(page.getByTestId('engine-list')).toContainText('Infra Redis')
 expect(state.queries.at(-1).engine_type).toBe('redis')
 await page.getByRole('button',{name:'查看监控',exact:true}).click()
 await expect(page.getByTestId('engine-value-redisPercent')).toHaveText('—')
 await expect(page.getByTestId('engine-value-redisLimit')).toHaveText('未设置内存上限')
 await page.getByRole('tab',{name:'吞吐与命中',exact:true}).click()
 state.setMode('no-lookups');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('engine-value-redisHitPercent')).toHaveText('—')
 await page.getByRole('button',{name:'返回引擎列表',exact:true}).click()
 await expect(page).toHaveURL(/engine_type=redis/)
 await expect(page.getByTestId('engine-list')).toBeVisible()
})

for(const automatic of [false,true])test(`Redis ${automatic?'automatic':'manual'} refresh keeps data, zoom and DOM`,async({page})=>{
 trackResourceChart(page)
 const state=await engineBackend(page,{owner:{...engineOwner,engine_type:'redis',display_name:'Infra Redis'}})
 if(automatic)await page.clock.install()
 await page.goto(`/engine-resources?engine_type=redis&deployment_id=${engineID}&refresh=${automatic?'60':'off'}`)
 const chart=page.getByTestId('engine-trend').locator('.chart-renderer')
 await expect(chart.locator('canvas')).toBeVisible();await dragResourceChartZoom(page,0.55,chart)
 const beforeZoom=(await resourceChartState(page,null,chart)).start
 const canvas=await chart.locator('canvas').elementHandle()
 const reads=()=>state.calls.filter(path=>path.endsWith('_observations')).length
 const before=reads();state.hold();state.setCapacity(2*1024**3)
 if(automatic)await page.clock.fastForward(60001)
 else await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect.poll(reads).toBe(before+1)
 await expect(page.getByTestId('engine-value-redisAllocated')).toHaveText('1.00 GiB')
 expect(await canvas.evaluate(node=>node.isConnected)).toBe(true)
 state.release();await expect(page.getByTestId('engine-value-redisAllocated')).toHaveText('2.00 GiB')
 expect(await canvas.evaluate(node=>node.isConnected)).toBe(true)
 expect((await resourceChartState(page,null,chart)).start).toBeCloseTo(beforeZoom,1)
})


for (const locale of ['zh-cn', 'en']) test(`Redis over-limit memory remains visible in list, detail and trend (${locale})`, async ({ page }) => {
 trackResourceChart(page)
 const state = await engineBackend(page, { locale, owner: { ...engineOwner, engine_type: 'redis', display_name: 'Infra Redis' }, mode: 'over-limit' })
 await page.goto('/engine-resources?engine_type=redis&refresh=off')
 await expect(page.getByTestId('engine-list')).toContainText('180.00 %')
 await page.getByRole('button', { name: locale === 'en' ? 'View monitoring' : '查看监控', exact: true }).click()
 await expect(page.getByTestId('engine-value-redisPercent')).toHaveText('180.00 %')
 const chart = page.getByTestId('engine-trend').locator('.chart-renderer')
 await expect(chart.locator('canvas')).toBeVisible()
 const props = await resourceChartProps(page, chart)
 expect(props.rows.filter(row => row.value !== null).every(row => row.value === 180)).toBe(true)
 await hoverResourceChartPoint(page, 0, chart)
 await expect(resourceChartTooltip(page, chart)).toContainText('180.00 %')
 state.setMode('no-limit')
 await page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
 await expect(page.getByTestId('engine-value-redisPercent')).toHaveText('—')
 await expect(page.getByTestId('engine-value-redisLimit')).toHaveText(locale === 'en' ? 'No Redis memory limit' : '未设置内存上限')
})
