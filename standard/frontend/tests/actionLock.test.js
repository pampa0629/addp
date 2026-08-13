import { describe, expect, it } from 'vitest'
import { useActionLock } from '../src/composables/useActionLock'

describe('action lock', () => {
  it('同一个资源键在首个异步操作完成前只执行一次', async () => {
    let release
    let calls = 0
    const pending = new Promise(resolve => { release = resolve })
    const { isLocked, runLocked } = useActionLock()

    const first = runLocked('glossary:21', async () => {
      calls += 1
      await pending
    })
    const second = runLocked('glossary:21', async () => {
      calls += 1
    })

    expect(calls).toBe(1)
    expect(isLocked('glossary:21')).toBe(true)
    release()
    await Promise.all([first, second])
    expect(isLocked('glossary:21')).toBe(false)
  })

  it('不同资源键互不阻塞，并在失败后释放锁', async () => {
    const { isLocked, runLocked } = useActionLock()
    const failure = runLocked('domain:1', async () => {
      throw new Error('failed')
    })
    const success = runLocked('domain:2', async () => 'done')

    await expect(failure).rejects.toThrow('failed')
    await expect(success).resolves.toBe('done')
    expect(isLocked('domain:1')).toBe(false)
    expect(isLocked('domain:2')).toBe(false)
  })

  it('服务端渲染环境不依赖 document', async () => {
    const { runLocked } = useActionLock()

    await expect(runLocked('domain:1', async () => 'done')).resolves.toBe('done')
  })
})
