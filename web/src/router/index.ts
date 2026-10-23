import { createRouter, createWebHistory } from 'vue-router'
import type { RouteMeta } from 'vue-router'

import { i18n } from '@/lib/i18n'
import { useSessionStore } from '@/stores/session'

declare module 'vue-router' {
  interface RouteMeta {
    /** Translation key of the document title. */
    titleKey?: string
    /** Constrain the work column to a form width. */
    narrow?: boolean
    /** Mount the publisher/product rail beside the page. */
    sidebar?: boolean
  }
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', redirect: '/marketplace' },
    {
      path: '/marketplace',
      name: 'catalog',
      component: () => import('@/views/CatalogView.vue'),
      meta: { titleKey: 'app.titles.catalog' },
    },
    {
      path: '/marketplace/:productId',
      name: 'product',
      component: () => import('@/views/ProductView.vue'),
      meta: { titleKey: 'app.titles.product' },
    },
    {
      path: '/workspace',
      name: 'workspace',
      component: () => import('@/views/WorkspaceView.vue'),
      meta: { titleKey: 'app.titles.workspace' },
    },
    {
      // The console is one route tree per product: the tabs switch state, not
      // the address, so a reload lands back on the same product.
      path: '/workspace/:productId',
      name: 'console',
      component: () => import('@/views/ConsoleView.vue'),
      meta: { titleKey: 'app.titles.console', sidebar: true },
    },
    {
      path: '/workspace/:productId/releases/:releaseId',
      name: 'release',
      component: () => import('@/views/ReleaseView.vue'),
      meta: { titleKey: 'app.titles.release', sidebar: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { titleKey: 'app.titles.notFound', narrow: true },
    },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

export function applyRouteTitle(meta: RouteMeta) {
  const key = meta.titleKey
  const title = key ? i18n.global.t(key) : ''
  document.title = title ? `${title} · Express` : 'Express'
}

router.afterEach((to) => {
  applyRouteTitle(to.meta)
  // A gated page reached by a direct link is not a dead end: resolve the
  // session before the view decides whether to offer sign-in. Boot also
  // exchanges a hand-off code from the address bar, so the first paint after
  // sign-in already has the account.
  const session = useSessionStore()
  if (session.state === 'loading') void session.boot()
})

export default router
