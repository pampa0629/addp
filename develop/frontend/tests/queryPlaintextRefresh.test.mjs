import {describe,it} from 'node:test'
import assert from 'node:assert/strict'
import {createQueryPlaintextRefresh} from '../src/utils/queryPlaintextRefresh.mjs'

describe('current authorization for visible query history',()=>{
  it('discards an in-flight recheck after the user clears or replaces the result',async()=>{
    let callback,resolve;const events=[]
    const refresh=createQueryPlaintextRefresh({now:()=>1000,setTimer:fn=>{callback=fn;return 1},clearTimer:()=>{},fetchExecution:()=>new Promise(done=>{resolve=done}),normalize:value=>value,onClear:()=>events.push('clear'),onResult:()=>events.push('result')})
    refresh.schedule({execution_id:'one',plaintext_expires_at:new Date(20000).toISOString()})
    const pending=callback()
    refresh.stop()
    resolve({plaintext:true})
    await pending
    assert.deepEqual(events,[])
  })
  it('clears plaintext at its deadline before reading the current protected result',async()=>{
    let clock=1000,callback,delay;const events=[]
    const refresh=createQueryPlaintextRefresh({now:()=>clock,setTimer:(fn,ms)=>{callback=fn;delay=ms;return 1},clearTimer:()=>{},fetchExecution:async()=>{events.push('read');return {masked:true}},normalize:value=>value,onClear:()=>events.push('clear'),onResult:result=>events.push(result.masked?'masked':'raw')})
    refresh.schedule({execution_id:'one',plaintext_expires_at:new Date(2000).toISOString()})
    assert.equal(delay,1000);clock=2000;await callback()
    assert.deepEqual(events,['clear','read','masked'])
  })
  it('clears stale values on a failed authorization recheck and stops after unmount',async()=>{
    let callback;const events=[]
    const refresh=createQueryPlaintextRefresh({now:()=>1000,setTimer:fn=>{callback=fn;return 1},clearTimer:()=>{},fetchExecution:async()=>{throw Error('denied')},normalize:value=>value,onClear:()=>events.push('clear'),onResult:()=>events.push('result')})
    refresh.schedule({execution_id:'one',plaintext_expires_at:new Date(20000).toISOString()});await callback();assert.deepEqual(events,['clear'])
    refresh.schedule({execution_id:'one',plaintext_expires_at:new Date(20000).toISOString()});refresh.dispose();await callback();assert.deepEqual(events,['clear'])
  })
})
