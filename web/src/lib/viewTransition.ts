import type { Router } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'

/**
 * Runs a view-changing update inside a same-document view transition, so the
 * browser cross-fades the old and new layout instead of swapping it.
 *
 * The transition is a progressive enhancement: a browser without
 * `startViewTransition`, or a reader who asked for reduced motion, gets the
 * update immediately. The returned promise always resolves after the update,
 * never rejects because the animation was skipped.
 *
 * `viewTransition` on a `RouterLink` covers ordinary navigation; this helper
 * exists for the two cases a link cannot express — a programmatic `push` and a
 * local state change such as switching a console tab.
 */
export async function withViewTransition(update: () => unknown): Promise<void> {
  const reduced =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

  if (typeof document === 'undefined' || !document.startViewTransition || reduced) {
    await update()
    return
  }

  const transition = document.startViewTransition(() => update())
  // The update is what the caller is waiting for, so its failure propagates
  // (a navigation that threw is not a successful one). The animation itself is
  // best effort: a transition superseded by a newer navigation still applied
  // the update, and `finished` rejects for that case. An implementation that
  // returns no promises at all still gets its callback run, so the wait is
  // skipped rather than throwing on a missing field.
  await transition?.updateCallbackDone
  await transition?.finished?.catch(() => undefined)
}

/** Navigates with a view transition. */
export function navigate(router: Router, to: RouteLocationRaw): Promise<void> {
  return withViewTransition(() => router.push(to))
}
