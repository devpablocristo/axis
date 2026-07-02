import { describe, it, expect } from 'vitest'
import { load, empty, type LoadState } from './load'

function recorder<T>(initial: T) {
  let state: LoadState<T> = empty(initial)
  const states: LoadState<T>[] = []
  const setState = (v: LoadState<T> | ((prev: LoadState<T>) => LoadState<T>)) => {
    state = typeof v === 'function' ? (v as (p: LoadState<T>) => LoadState<T>)(state) : v
    states.push(state)
  }
  return { get: () => state, states, setState }
}

describe('load', () => {
  it('keeps previous data visible while loading (no blank flash)', async () => {
    const r = recorder<string[]>(['old'])
    await load(r.setState, async () => ['new'], [])
    // First setState (loading) must preserve prev data, not reset to fallback []
    expect(r.states[0]).toEqual({ data: ['old'], error: '', loading: true })
    expect(r.get()).toEqual({ data: ['new'], error: '', loading: false })
  })

  it('preserves last-good data on error', async () => {
    const r = recorder<string[]>(['keep'])
    await load(r.setState, async () => {
      throw new Error('boom')
    }, [])
    expect(r.get().data).toEqual(['keep'])
    expect(r.get().loading).toBe(false)
    expect(r.get().error).toBe('boom')
  })
})
