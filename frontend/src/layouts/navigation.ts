import type { IconName } from '@/components'

/**
 * The navigation, taken from `Legacy/Views/Shared/_SideBar.cshtml`.
 *
 * Order, labels and grouping are the predecessor's, so an operator moving across finds
 * things where they expect them. The labels are the predecessor's too even where the
 * controller is named differently: the menu says "DB Resource Mgmt" and the
 * controller says `ExecutionPlan`, and the operators know the menu.
 *
 * Unbuilt entries stay listed here with their FEAT number, because this list is
 * also the port's inventory and featuresIn reads it for the docs. They are shown
 * to nobody, administrators included: the rail is a working menu, and an entry
 * that cannot be clicked is a promise rather than a menu item. What is left to
 * build is docs/features/README.md's to say.
 */

/** A role that gates a menu entry. Matches the predecessor's account flags. */
export type Role = 'administrator' | 'dba' | 'release_approver'

/**
 * What the signed-in operator holds, and what this deployment switched on.
 *
 * Three things gate the rail now. The third is not about the operator at all: a
 * deployment that has brought no database under the write or the release path
 * answers every endpoint there saying so, and the menu offered five entries
 * whose whole content was a red line explaining that. Same failure the
 * permission gate above exists to prevent, from the other direction.
 */
export type Caller = {
  roles: readonly string[]
  permissions: readonly string[]
  /**
   * Required, not optional. It was optional for one commit, and in that commit
   * the shell built its argument field by field and left this one out — so
   * every entry gated on a feature disappeared for everybody, and the types
   * were happy. An absent capability list and an empty one mean the same thing
   * to the filter and opposite things to the reader.
   */
  features: readonly string[]
}

export type NavLeaf = {
  label: string
  feature: string
  /** Present once the page exists. Absent means not built yet. */
  route?: string
  /** Absent means visible to everyone who can sign in. */
  roles?: Role[]
  /**
   * The permission the page's endpoints enforce, when it needs one.
   *
   * Roles are not enough. A release approver holds no query permission, so the
   * Query entry was offered to them and answered 403 when they clicked it — a
   * menu that lies about what it opens. This is the same string the server
   * checks, so the rail and the endpoint cannot drift into disagreeing.
   *
   * A list means any one of them opens the page.
   */
  permission?: string | readonly string[]
  /**
   * The deployment capability this page needs, when it needs one.
   *
   * Absent means the page works wherever Avior runs. `write` and `release` are
   * the two that a deployment can be without, and the server reports which it
   * has on the session.
   */
  requires?: 'write' | 'release' | 'provision'
  note?: string
}

export type NavItem = NavLeaf & { icon: IconName }

export type NavGroup = {
  label: string
  icon: IconName
  roles?: Role[]
  children: NavLeaf[]
}

export type NavEntry = NavItem | NavGroup

export function isGroup(entry: NavEntry): entry is NavGroup {
  return 'children' in entry
}

export const navigation: NavEntry[] = [
  {
    label: 'Query',
    icon: 'terminal',
    route: 'query-console',
    feature: 'FEAT-002',
    permission: 'query.inspect',
  },
  {
    label: 'Update Query',
    icon: 'file-pen',
    route: 'write-compose',
    requires: 'write',
    feature: 'FEAT-009',
    permission: 'query.execute',
  },
  {
    label: 'Update Requests',
    icon: 'pull-request',
    route: 'write-requests',
    requires: 'write',
    feature: 'FEAT-009',
    permission: 'query.execute',
  },
  { label: 'Export Data', icon: 'table', feature: 'FEAT-010' },
  {
    label: 'Restore DB',
    icon: 'database',
    feature: 'FEAT-016',
    // The predecessor hides this entirely when the isStg app setting is present, so it
    // never appears on staging.
    note: 'Hidden on staging in the predecessor',
  },
  {
    label: 'Database Objects',
    icon: 'database',
    children: [
      /*
       * Table first, because it answers the question the other five are read
       * around: what does this hold. The predecessor had no such entry — its "Table"
       * menu was the index list, which FEAT-023 recovered and which is still
       * below.
       */
      {
        label: 'Table',
        feature: 'FEAT-054',
        route: 'objects-table',
        permission: 'query.inspect',
      },
      {
        label: 'View',
        feature: 'FEAT-023',
        route: 'objects-view',
        permission: 'query.inspect',
      },
      {
        label: 'Stored Procedure',
        feature: 'FEAT-023',
        route: 'objects-stored-procedure',
        permission: 'query.inspect',
      },
      {
        label: 'User Defined Type',
        feature: 'FEAT-023',
        route: 'objects-user-defined-type',
        permission: 'query.inspect',
      },
      {
        label: 'Function',
        feature: 'FEAT-023',
        route: 'objects-function',
        permission: 'query.inspect',
      },
      {
        label: 'Index',
        feature: 'FEAT-023',
        route: 'objects-index',
        permission: 'query.inspect',
      },
    ],
  },
  {
    label: 'Lineage',
    icon: 'workflow',
    route: 'lineage',
    feature: 'FEAT-070',
    permission: 'query.inspect',
  },
  {
    label: 'Observatory',
    icon: 'telescope',
    route: 'observatory',
    feature: 'FEAT-071',
  },
  {
    label: 'Application Setting',
    icon: 'wrench',
    children: [
      { label: 'Simple Setting', feature: 'FEAT-013' },
      { label: 'Scheduler Setting', feature: 'FEAT-018' },
      { label: 'DB Configuration', feature: 'FEAT-012' },
      { label: 'Satan Configuration', feature: 'FEAT-020' },
    ],
  },
  {
    label: 'Release',
    icon: 'release',
    children: [
      /*
       * Two entries, not the predecessor's three, and they answer two different
       * questions: what is waiting for me, and what happened.
       *
       * "DB Compare" is gone because Avior does not compare — ADR-0004 leaves
       * generation and comparison in the database project's CI and keeps the
       * governance here. "SP Release Approve" is gone because the two approval
       * queues were two release mechanisms, and on PostgreSQL a table and a
       * function both reach production as a migration, so there is one queue.
       *
       * An empty pending list is a meaningful answer — nothing needs you —
       * and it stops being one when it is mixed into a history.
       */
      { label: 'Pending', feature: 'FEAT-017', route: 'release-pending', requires: 'release' },
      { label: 'History', feature: 'FEAT-017', route: 'release-history', requires: 'release' },
      /*
       * A third question the first two cannot answer. History lists releases —
       * what was submitted and decided. This lists what a database actually
       * holds, reconciled against the database's own ledger, so a change made
       * outside Avior appears here and nowhere else.
       */
      { label: 'Applied', feature: 'FEAT-039', route: 'release-applied', requires: 'release' },
      /*
       * FEAT-059. Read before approving rather than after applying: a database
       * that does not exist yet is the one case the queue cannot show, because
       * nothing has failed yet.
       */
      {
        label: 'Provisioning',
        feature: 'FEAT-059',
        route: 'release-provisioning',
        permission: 'provision.manage',
        requires: 'provision',
      },
    ],
  },
  {
    label: 'Admin',
    icon: 'users',
    /*
     * No role on the group. Every built entry under it carries the permission
     * its own endpoints enforce, and those no longer belong to one role:
     * FEAT-058 gave `permission.manage` to the DBA as well, and a group gated
     * on `administrator` hid the page from the people it was given to. The
     * unbuilt entries are still administrator-only, through the rule about
     * entries with no route, and a group left with no visible children
     * disappears on its own.
     */
    children: [
      {
        label: 'Account Management',
        feature: 'FEAT-004',
        route: 'admin-accounts',
        permission: 'account.manage',
      },
      {
        label: 'Service Accounts',
        feature: 'FEAT-063',
        route: 'admin-service-accounts',
        permission: 'account.manage',
      },
      {
        label: 'Database Access',
        feature: 'FEAT-058',
        route: 'admin-databases',
        permission: 'permission.manage',
      },
      {
        label: 'Column Restrictions',
        feature: 'FEAT-055',
        route: 'admin-columns',
        permission: 'permission.manage',
      },
      { label: 'Log', feature: 'FEAT-003', route: 'audit-all', permission: 'audit.read.all' },
      /*
       * The predecessor listed "Update Query Mgmt" here as well as under DBA, because a
       * write request took two approvals. Only the DBA's ran the statement; the
       * administrator's recorded a decision and executed nothing. FEAT-009
       * ports the one that gates, so this entry has nothing left to open and is
       * gone rather than pointing at somebody else's queue.
       */
      { label: 'DB Resource Mgmt', feature: 'FEAT-005' },
      { label: 'Restart', feature: 'FEAT-052' },
    ],
  },
  {
    label: 'DBA',
    icon: 'server',
    roles: ['dba'],
    children: [
      { label: 'DNS Mapping', feature: 'FEAT-032' },
      { label: 'Online Reindex', feature: 'FEAT-031' },
      {
        label: 'Update Query Mgmt',
        feature: 'FEAT-009',
        route: 'write-queue',
        requires: 'write',
        permission: 'write.approve',
      },
      {
        // Not the predecessor's: it kept its backup tables in a database of their own
        // and nothing listed them. FEAT-009 keeps them beside the data they
        // came from, and this is where somebody sees what has accumulated.
        label: 'Backup Tables',
        feature: 'FEAT-009',
        route: 'write-captures',
        requires: 'write',
        permission: 'instance.maintain',
      },
      { label: 'Monitor', feature: 'FEAT-038' },
      { label: 'Table Release Approve', feature: 'FEAT-036' },
    ],
  },
  { label: 'Healthy Check', icon: 'list-checks', feature: 'FEAT-030' },
  {
    label: 'Personal Log',
    icon: 'history',
    route: 'audit-mine',
    feature: 'FEAT-003',
    permission: 'audit.read.own',
  },
]

/**
 * visibleTo filters the navigation by the caller's roles.
 *
 * A group whose own roles exclude the caller disappears with its children. A
 * child with its own roles is dropped individually, and a group left with no
 * children disappears too rather than showing an empty expander.
 *
 * An entry with no route is not built, and is dropped for everybody,
 * administrators included: an entry that cannot be clicked is not a menu item,
 * it is a promise.
 *
 * An unrecognised role in the caller's list is ignored rather than treated as a
 * wildcard: a role the directory knows and this build does not must not open
 * anything.
 */
export function visibleTo(entries: NavEntry[], caller: Caller): NavEntry[] {
  const heldRoles = new Set(caller.roles)
  const heldPermissions = new Set(caller.permissions)
  const permitted = (required?: Role[]) =>
    required === undefined || required.some((role) => heldRoles.has(role))

  const heldFeatures = new Set(caller.features ?? [])

  // Three questions, and all have to be yes. A page that is not built cannot be
  // opened; a page whose endpoints will refuse this caller opens onto a 403; and
  // a page belonging to a feature this deployment never configured opens onto a
  // notice saying so. Offering any of them is a menu that lies about what it
  // does.
  const reachable = (entry: NavLeaf) =>
    entry.route !== undefined &&
    holds(heldPermissions, entry.permission) &&
    (entry.requires === undefined || heldFeatures.has(entry.requires))

  return entries.flatMap<NavEntry>((entry): NavEntry[] => {
    if (!permitted(entry.roles)) return []
    if (!isGroup(entry)) return reachable(entry) ? [entry] : []

    const children = entry.children.filter((child) => permitted(child.roles) && reachable(child))
    // A group left with nothing disappears rather than showing an empty
    // expander that opens onto nothing.
    if (children.length === 0) return []
    return [{ ...entry, children }]
  })
}

/** holds answers whether the permissions held open what an entry needs. */
export const holds = (held: ReadonlySet<string>, needed?: string | readonly string[]) =>
  needed === undefined || (typeof needed === 'string' ? [needed] : needed).some((p) => held.has(p))

/** featuresIn lists every FEAT the navigation references, for the docs to check. */
export function featuresIn(entries: NavEntry[]): string[] {
  const out = new Set<string>()
  for (const entry of entries) {
    if (isGroup(entry)) {
      entry.children.forEach((child) => out.add(child.feature))
    } else {
      out.add(entry.feature)
    }
  }
  return [...out].sort()
}

/**
 * permissionFor returns what a route needs, by the name it is registered under.
 *
 * The rail is the one place that says which permission opens which page, so the
 * router asks it rather than keeping a second list. A second list would drift,
 * and the drift would show as a menu entry that opens onto a refusal — which is
 * the thing the rail exists to prevent.
 *
 * A route the rail does not name needs nothing. Those are the pages reached by
 * following a link rather than by choosing from the menu: one release, one
 * table's structure. What they hold is guarded by their own endpoints.
 */
export function permissionFor(route: string): string | readonly string[] | undefined {
  for (const entry of navigation) {
    const leaves = isGroup(entry) ? entry.children : [entry]
    for (const leaf of leaves) {
      if (leaf.route === route) return leaf.permission
    }
  }
  return undefined
}

/**
 * firstReachable is where somebody lands when they asked for a page they cannot
 * open, and where they start when they arrive with no destination.
 *
 * The first entry of the rail they can actually use, in the rail's own order,
 * which is the predecessor's — so the answer is the one they would have clicked. Null
 * means the rail is empty: an operator who holds no role reaches nothing, which
 * FEAT-007 makes an ordinary state for somebody on their first day.
 */
/*
 * The observatory is open to every signed-in operator and sits early in the
 * rail, so it would otherwise become the landing page of anybody whose own
 * pages come later, a release approver first among them.
 */
const notLanding = 'observatory'

export function firstReachable(caller: Caller): string | null {
  for (const entry of visibleTo(navigation, caller)) {
    const leaves = isGroup(entry) ? entry.children : [entry]
    for (const leaf of leaves) {
      if (leaf.route !== undefined && leaf.route !== notLanding) return leaf.route
    }
  }
  return null
}
