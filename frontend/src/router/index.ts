import { createRouter, createWebHistory } from 'vue-router'
import type { RouteLocationNormalized, RouteRecordRaw } from 'vue-router'

import { firstReachable, holds, permissionFor } from '@/layouts/navigation'
import { useSessionStore } from '@/stores/session'

/**
 * FEAT-023. The predecessor gives each object type its own menu entry and its own URL,
 * and the operators navigate by those, so the five share one view rather than
 * one route with a parameter the menu would have to build.
 */
const objectTypes = [
  ['view', 'objects-view'],
  ['stored_procedure', 'objects-stored-procedure'],
  ['user_defined_type', 'objects-user-defined-type'],
  ['function', 'objects-function'],
  ['index', 'objects-index'],
] as const

/**
 * The table viewer is its own route, not a sixth object type.
 *
 * The other five return a script and share one page. A table is four
 * independent facts and returns a document, so it is a different page — giving
 * it a type would make the shared one branch on which of its two shapes it was
 * showing. FEAT-054.
 */
const tableRoute: RouteRecordRaw = {
  path: 'objects/table',
  name: 'objects-table',
  component: () => import('@/features/objects/TableStructureView.vue'),
}

const objectRoutes: RouteRecordRaw[] = objectTypes.map(([objectType, name]) => ({
  path: `objects/${objectType.replace(/_/gu, '-')}`,
  name,
  component: () => import('@/features/objects/ObjectViewerView.vue'),
  props: { objectType },
}))

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/features/auth/LoginView.vue'),
    meta: { public: true },
  },
  {
    path: '/',
    component: () => import('@/layouts/AppShell.vue'),
    children: [
      {
        path: '',
        name: 'query-console',
        component: () => import('@/features/query/QueryConsoleView.vue'),
      },
      tableRoute,
      ...objectRoutes,
      // FEAT-070. The database and the focus are in the query, so a link opens it on one object.
      {
        path: 'lineage',
        name: 'lineage',
        component: () => import('@/features/lineage/LineageView.vue'),
      },
      // FEAT-071. The open tab is in the query, so a link opens the page on one view.
      {
        path: 'observatory',
        name: 'observatory',
        component: () => import('@/features/observatory/ObservatoryView.vue'),
      },
      /**
       * FEAT-003. The predecessor has two menu entries and two controllers over one
       * table, and the operators navigate by those, so the personal log and the
       * administrator's keep their own URLs over one view.
       */
      {
        path: 'audit',
        name: 'audit-mine',
        component: () => import('@/features/audit/AuditLogView.vue'),
        props: { scope: 'mine' },
      },
      {
        path: 'audit/all',
        name: 'audit-all',
        component: () => import('@/features/audit/AuditLogView.vue'),
        props: { scope: 'all' },
      },
      /*
       * FEAT-017. Two lists over one view, the way the audit log has two: what
       * is waiting for a decision, and what already happened. They are separate
       * URLs because they answer separate questions and an operator navigates by
       * them.
       *
       * One release has its own URL as well, because an approver reads it,
       * decides, and needs a link they can send somebody.
       */
      {
        path: 'releases',
        name: 'release-pending',
        component: () => import('@/features/release/ReleaseQueueView.vue'),
        props: { scope: 'pending' },
      },
      {
        path: 'releases/history',
        name: 'release-history',
        component: () => import('@/features/release/ReleaseQueueView.vue'),
        props: { scope: 'history' },
      },
      /*
       * FEAT-059. What a governed database needs before a release can reach
       * it. Under releases rather than under admin, because it is read on the
       * way to applying one: the question it answers is "can this land".
       */
      {
        path: 'releases/provisioning',
        name: 'release-provisioning',
        component: () => import('@/features/provision/ProvisionView.vue'),
      },
      /*
       * FEAT-009. Two routes over one component, the way the release queue is:
       * "mine" is what a developer asked for, "queue" is what is waiting for an
       * approver. An empty queue means nothing needs you, and stops meaning
       * that the moment it is mixed with settled requests.
       */
      {
        // The composer, and where the nav's "Update Query" lands: The predecessor's
        // top-level entry was its create page too.
        path: 'writes/new',
        name: 'write-compose',
        component: () => import('@/features/write/WriteComposeView.vue'),
      },
      {
        path: 'writes',
        name: 'write-requests',
        component: () => import('@/features/write/WriteRequestsView.vue'),
        props: { scope: 'mine' },
      },
      {
        path: 'writes/queue',
        name: 'write-queue',
        component: () => import('@/features/write/WriteRequestsView.vue'),
        props: { scope: 'queue' },
      },
      {
        path: 'writes/captures',
        name: 'write-captures',
        component: () => import('@/features/write/WriteCapturesView.vue'),
      },
      {
        // The database is a query parameter, not a path segment: the menu links
        // here without knowing one, and the page is where it is chosen.
        path: 'releases/applied',
        name: 'release-applied',
        component: () => import('@/features/release/AppliedMigrationsView.vue'),
      },
      /*
       * FEAT-004. Under admin/ because that is where the predecessor's account pages
       * live and the operators navigate by the menu, not by the URL.
       */
      /*
       * Reached from the account menu rather than from the rail. The rail is
       * the predecessor's and these are not in it; a menu entry and a rail entry for
       * the same page would be two places to keep in step.
       */
      {
        path: 'account',
        name: 'profile',
        meta: { title: 'Account' },
        component: () => import('@/features/profile/ProfileView.vue'),
      },
      {
        path: 'account/agent',
        name: 'agent-setup',
        meta: { title: 'Account · Connect your agent' },
        component: () => import('@/features/profile/AgentSetupView.vue'),
      },
      {
        path: 'account/tokens',
        name: 'tokens',
        meta: { title: 'Account · API tokens' },
        component: () => import('@/features/profile/TokensView.vue'),
      },
      {
        path: 'admin/accounts',
        name: 'admin-accounts',
        component: () => import('@/features/accounts/OperatorsView.vue'),
      },
      // FEAT-063. Accounts for agents that are nobody's person.
      {
        path: 'admin/service-accounts',
        name: 'admin-service-accounts',
        component: () => import('@/features/accounts/ServiceAccountsView.vue'),
      },
      // FEAT-058. Which roles reach which governed database.
      {
        path: 'admin/databases',
        name: 'admin-databases',
        component: () => import('@/features/accounts/DatabaseAccessView.vue'),
      },
      // FEAT-055. Which columns are restricted, and who was unlocked for them.
      {
        path: 'admin/columns',
        name: 'admin-columns',
        component: () => import('@/features/accounts/ColumnRestrictionsView.vue'),
      },
      {
        path: 'releases/:id',
        name: 'release-detail',
        component: () => import('@/features/release/ReleaseDetailView.vue'),
        props: true,
        meta: { title: 'Release · One release' },
      },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

/**
 * requireSession guards every route that has not opted out, matching the
 * backend's closed PublicPaths list. A new view is private until someone marks
 * it public, so forgetting to think about access control fails closed.
 */
export async function requireSession(
  to: Pick<RouteLocationNormalized, 'meta' | 'fullPath'>,
): Promise<true | { name: string; query: { redirect: string } }> {
  if (to.meta['public'] === true) return true

  const session = useSessionStore()
  if (session.caller === null) {
    await session.refresh()
  }
  if (session.caller === null) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  return true
}

/**
 * requireAccess sends an operator away from a page their permissions do not
 * open, rather than letting them arrive at one that refuses them.
 *
 * Typing a URL is the case: the rail already hides what somebody cannot use, so
 * this is for the address bar, a stale bookmark, and a link somebody was sent.
 * The destination is the first entry of their own rail — not a fixed page,
 * because a release approver holds no query permission and the query console
 * would refuse them just as thoroughly.
 */
export function requireAccess(
  to: Pick<RouteLocationNormalized, 'name' | 'meta'>,
): true | { name: string } {
  if (to.meta['public'] === true) return true

  const session = useSessionStore()
  const caller = session.caller
  if (caller === null) return true

  const name = typeof to.name === 'string' ? to.name : ''
  const needed = permissionFor(name)
  if (holds(new Set(caller.permissions), needed)) return true

  /*
   * Their own rail decides where they land.
   *
   * It cannot be empty today: the release ledger asks only for a session, so
   * every signed-in operator reaches at least that. The fallback exists for the
   * day somebody gives those entries a permission, and the test beside this one
   * is what makes that a decision rather than a surprise.
   */
  return { name: firstReachable(caller) ?? 'query-console' }
}

router.beforeEach(requireSession)
router.beforeEach(requireAccess)
