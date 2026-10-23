import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { withViewTransition } from '@/lib/viewTransition'

/**
 * The helper is a progressive enhancement, so the behaviour that matters is
 * what happens when the browser cannot animate: the update must still run, and
 * it must run exactly once. A transition that swallowed the update, or ran it
 * twice, would be a page that silently fails to navigate.
 */
describe('withViewTransition', () => {
  const original = document.startViewTransition
  const originalMatchMedia = window.matchMedia

  /**
   * A browser stub with the real shape: `startViewTransition` returns its
   * object synchronously and runs the callback inside it, so a test can tell
   * that the helper waited for the callback rather than for the animation.
   */
  function stubTransition(finish: Promise<void> = Promise.resolve()) {
    const start = vi.fn((callback: () => unknown) => {
      const updateCallbackDone = Promise.resolve(callback()).then(() => undefined)
      return {
        finished: finish,
        ready: Promise.resolve(),
        updateCallbackDone,
        skipTransition: vi.fn(),
        types: new Set<string>(),
      }
    })
    // @ts-expect-error a minimal stand-in for the browser API
    document.startViewTransition = start
    return start
  }

  beforeEach(() => {
    // jsdom ships no matchMedia, so the reduced-motion query has to be stubbed
    // before the helper can read it.
    window.matchMedia = vi.fn().mockReturnValue({ matches: false } as MediaQueryList)
  })

  afterEach(() => {
    document.startViewTransition = original
    window.matchMedia = originalMatchMedia
    vi.restoreAllMocks()
  })

  it('runs the update directly when the browser has no support', async () => {
    // @ts-expect-error simulating a browser without the API
    document.startViewTransition = undefined
    const update = vi.fn()

    await withViewTransition(update)

    expect(update).toHaveBeenCalledTimes(1)
  })

  it('wraps the update in a transition when one is available', async () => {
    const start = stubTransition()
    const update = vi.fn()

    await withViewTransition(update)

    expect(start).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledTimes(1)
  })

  it('still waits for an update that returns a promise', async () => {
    stubTransition()
    let settled = false

    const gate = Promise.withResolvers<void>()
    setTimeout(() => gate.resolve(), 0)
    await withViewTransition(async () => {
      await gate.promise
      settled = true
    })

    expect(settled).toBe(true)
  })

  it('resolves when the transition is skipped mid-flight', async () => {
    const skipped = Promise.reject(new Error('skipped'))
    // The helper installs the handler; an unhandled rejection here would fail
    // the whole run, which is exactly the bug this test guards against.
    stubTransition(skipped)
    const update = vi.fn()

    // An interrupted animation is not a failed navigation.
    await expect(withViewTransition(update)).resolves.toBeUndefined()
    expect(update).toHaveBeenCalledTimes(1)
  })

  it('skips the animation when the reader asked for reduced motion', async () => {
    const start = stubTransition()
    ;(window.matchMedia as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      matches: true,
    } as MediaQueryList)
    const update = vi.fn()

    await withViewTransition(update)

    expect(start).not.toHaveBeenCalled()
    expect(update).toHaveBeenCalledTimes(1)
  })
})
