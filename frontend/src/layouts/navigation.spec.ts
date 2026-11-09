import { describe, expect, it } from 'vitest'

import { featuresIn, isGroup, navigation, visibleTo, type NavEntry } from './navigation'

/*
 * What each role holds, mirroring the server's identity.rolePermissions.
 *
 * The frontend never derives this — the session carries it from /auth/me — so
 * a test has to state it. That it still matches the server is proved by the
 * end-to-end cases, which sign in as real seeded operators; this only fixes the
 * input so the filter itself can be tested.
 */
const permissionsOf: Record<string, string[]> = {
  developer: ['query.inspect', 'query.execute', 'audit.read.own'],
  dba: [
    'query.inspect',
    'query.execute',
    'audit.read.own',
    'instance.read',
    'instance.maintain',
    // FEAT-009. The predecessor sent write approvals to the DBA, and its path is the
    // one that actually ran the statement.
    'write.approve',
  ],
  administrator: [
    'query.inspect',
    'audit.read.all',
    'permission.manage',
    'threshold.manage',
    'account.manage',
  ],
  release_approver: ['release.approve'],
  update_approver: ['write.approve'],
}

/*
 * A caller on a deployment that has everything switched on.
 *
 * The cases below are about roles and permissions, so the deployment has to be
 * out of the way: a rail entry can now also be hidden because this Avior was
 * never given a credential for that path, and the cases for that are their own,
 * further down.
 */
function as(...roles: string[]) {
  return {
    roles,
    permissions: [...new Set(roles.flatMap((role) => permissionsOf[role] ?? []))],
    features: ['write', 'release'],
  }
}

function labels(entries: NavEntry[]): string[] {
  return entries.map((entry) => entry.label)
}

function group(entries: NavEntry[], label: string) {
  const found = entries.find((entry) => entry.label === label)
  if (found === undefined || !isGroup(found)) throw new Error(`no group ${label}`)
  return found
}

describe('navigation', () => {
  // The predecessor's own order, from _SideBar.cshtml. An operator moving across should
  // find things where they expect them, so a reordering here is a decision and
  // this test is where it gets noticed.
  // The predecessor's order, with the unbuilt entries removed rather than reordered:
  // an operator moving across still finds what exists where they expect it.
  it('follows the predecessor menu order for a plain developer', () => {
    expect(labels(visibleTo(navigation, as('developer')))).toEqual([
      'Query',
      'Update Query',
      'Update Requests',
      'Database Objects',
      'Lineage',
      'Observatory',
      'Release',
      'Personal Log',
    ])
  })

  // FEAT-071: the owner opened the observatory to every signed-in operator on 2026-09-27.
  it('offers the observatory to every signed-in operator', () => {
    for (const role of ['developer', 'dba', 'administrator', 'release_approver']) {
      expect(labels(visibleTo(navigation, as(role))), role).toContain('Observatory')
    }
  })

  // FEAT-070 AC-28: beside the objects it is read around, behind the permission its endpoints ask.
  it('puts lineage beside database objects, for whoever may inspect', () => {
    const seen = labels(navigation)
    expect(seen.indexOf('Lineage')).toBe(seen.indexOf('Database Objects') + 1)
    expect(labels(visibleTo(navigation, as('release_approver')))).not.toContain('Lineage')
  })

  // The rail is the port's progress to whoever is porting it, and a working
  // menu to everybody else. An entry that cannot be clicked is not a menu item,
  // it is a promise.
  it('shows the unbuilt entries to nobody, administrators included', () => {
    for (const role of ['administrator', 'developer', 'dba']) {
      const seen = labels(visibleTo(navigation, as(role)))
      for (const unbuilt of ['Export Data', 'Restore DB', 'Healthy Check', 'Application Setting']) {
        expect(seen, `${role} sees ${unbuilt}`).not.toContain(unbuilt)
      }
    }
    const admin = group(visibleTo(navigation, as('administrator')), 'Admin')
    const offered = admin.children.map((child) => child.label)
    expect(offered).not.toContain('DB Resource Mgmt')
    expect(offered).not.toContain('Restart')
  })

  it('leaves an operator nothing they cannot open', () => {
    for (const role of ['developer', 'dba', 'release_approver', 'administrator']) {
      for (const entry of visibleTo(navigation, as(role))) {
        if (isGroup(entry)) {
          for (const child of entry.children) {
            expect(child.route, `${child.label} is offered but not built`).toBeDefined()
          }
          continue
        }
        expect(entry.route, `${entry.label} is offered but not built`).toBeDefined()
      }
    }
  })

  // The predecessor's five, plus the one it had no page for. Its "Table" menu was the
  // index list — still below — so an operator wanting to know what a column
  // held read a CREATE INDEX statement and inferred.
  it('names the five object types the predecessor offers, and the table page it lacked', () => {
    expect(group(navigation, 'Database Objects').children.map((c) => c.label)).toEqual([
      'Table',
      'View',
      'Stored Procedure',
      'User Defined Type',
      'Function',
      'Index',
    ])
  })

  it('names the five application settings', () => {
    expect(group(navigation, 'Application Setting').children.map((c) => c.label)).toEqual([
      'Simple Setting',
      'Scheduler Setting',
      'DB Configuration',
      'Satan Configuration',
    ])
  })

  it('uses the predecessor labels rather than controller names', () => {
    const admin = group(navigation, 'Admin').children.map((c) => c.label)

    // The controller is ExecutionPlan; the menu says DB Resource Mgmt, and the
    // operators know the menu.
    expect(admin).toContain('DB Resource Mgmt')
    expect(admin).not.toContain('Execution Plan')
  })

  // Counts the children too: a group's entries are where routes land as the
  // port proceeds, and a filter that only saw the top level would report
  // "nothing is built" long after things were.
  it('routes only what is built, top level and children alike', () => {
    const routed: string[] = []
    for (const entry of navigation) {
      if (isGroup(entry)) {
        routed.push(...entry.children.filter((c) => c.route !== undefined).map((c) => c.label))
        continue
      }
      if (entry.route !== undefined) routed.push(entry.label)
    }

    expect(routed).toEqual([
      'Query',
      'Update Query',
      'Update Requests',
      'Table',
      'View',
      'Stored Procedure',
      'User Defined Type',
      'Function',
      'Index',
      'Lineage',
      'Observatory',
      'Pending',
      'History',
      'Applied',
      'Provisioning',
      'Account Management',
      'Service Accounts',
      'Database Access',
      'Column Restrictions',
      'Log',
      'Update Query Mgmt',
      'Backup Tables',
      'Personal Log',
    ])
  })
})

describe('visibleTo', () => {
  /*
   * The Admin group used to be gated on the administrator role, which was a
   * proxy for "these need administrative permissions" — and stopped being true
   * when FEAT-058 gave permission.manage to the DBA. The page was built, the
   * endpoints let them in, and the rail hid it.
   */
  it('shows a dba the admin entries their permissions actually open', () => {
    const dba = {
      roles: ['dba'],
      permissions: ['permission.manage', 'query.inspect', 'query.execute'],
      features: ['write', 'release'],
    }
    const admin = group(visibleTo(navigation, dba), 'Admin')
    const seen = admin.children.map((child) => child.label)

    expect(seen).toContain('Database Access')
    // And nothing else under Admin: those need permissions a DBA does not
    // hold, and the unbuilt ones are shown to nobody.
    expect(seen).toEqual(['Database Access', 'Column Restrictions'])
  })

  it('hides the Admin and DBA groups from a developer', () => {
    const seen = labels(visibleTo(navigation, as('developer')))

    expect(seen).not.toContain('Admin')
    expect(seen).not.toContain('DBA')
  })

  it('shows the Admin group to an administrator', () => {
    expect(labels(visibleTo(navigation, as('administrator')))).toContain('Admin')
  })

  /*
   * The DBA group used to be empty of built pages, so a dba saw no group at
   * all. FEAT-009 put the backup listing in it, and the group now appears —
   * the gate that still holds is the role: an administrator who is not a dba
   * does not see it, however much of it exists.
   */
  it('opens the DBA group to a dba and to nobody else', () => {
    const dba = group(visibleTo(navigation, as('dba')), 'DBA')
    // Both, now that a DBA may decide a write request as well as prune what one
    // kept — which is the pair the predecessor gave them.
    expect(dba.children.map((child) => child.label)).toEqual(['Update Query Mgmt', 'Backup Tables'])

    expect(labels(visibleTo(navigation, as('administrator')))).not.toContain('DBA')
  })

  // Update Query Mgmt is the approver's queue, and lives in a group gated on
  // the DBA role. Somebody holding write.approve and nothing else does not see
  // it, which is the rail agreeing with the predecessor's grouping rather than with the
  // permission alone.
  it('offers the approver queue to a dba who may decide', () => {
    const both = {
      roles: ['dba'],
      permissions: ['instance.maintain', 'write.approve'],
      features: ['write', 'release'],
    }
    const dba = group(visibleTo(navigation, both), 'DBA')

    expect(dba.children.map((child) => child.label)).toEqual(['Update Query Mgmt', 'Backup Tables'])
  })

  it('shows both groups to someone holding both roles', () => {
    const seen = labels(visibleTo(navigation, as('dba', 'administrator')))

    expect(seen).toContain('Admin')
    expect(seen).toContain('DBA')
  })

  /*
   * The release lists are not gated by role, and that is deliberate rather than
   * an omission. Reading what is waiting and what reached production is not the
   * same act as letting something through: the decision is gated on the
   * release.approve permission at the page and at the endpoint, so hiding the
   * list as well would only stop people seeing what was done in their name.
   */
  it('shows every release list to every operator', () => {
    for (const role of ['developer', 'release_approver', 'dba']) {
      const release = group(visibleTo(navigation, as(role)), 'Release')
      expect(release.children.map((c) => c.label)).toEqual(['Pending', 'History', 'Applied'])
    }
  })

  // A role the directory knows and this build does not must not open anything.
  // It carries no permissions either, so it reaches nothing a permission gates.
  it('ignores a role it does not recognise', () => {
    const seen = labels(visibleTo(navigation, as('auditor', 'wheel', 'root')))

    expect(seen).not.toContain('Admin')
    expect(seen).not.toContain('DBA')
    expect(seen).not.toContain('Query')
    // Not an empty rail, and not an error: what needs no permission is still
    // there, which is what says the unknown role was ignored rather than fatal.
    expect(seen).toContain('Release')
  })

  it('shows the ungated entries to a caller holding nothing', () => {
    const seen = labels(visibleTo(navigation, as()))

    expect(seen).toContain('Release')
    expect(seen).not.toContain('Query')
    expect(seen).not.toContain('Admin')
  })

  // An expander that opens onto nothing is worse than no expander.
  it('drops a group left with no visible children', () => {
    const entries: NavEntry[] = [
      {
        label: 'Everything gated',
        icon: 'lock',
        children: [
          {
            label: 'Only for admins',
            feature: 'FEAT-000',
            route: 'admin-accounts',
            roles: ['administrator'],
          },
        ],
      },
    ]

    expect(visibleTo(entries, as('developer'))).toEqual([])
    expect(visibleTo(entries, as('administrator'))).toHaveLength(1)
  })

  it('leaves the original untouched', () => {
    const before = JSON.stringify(navigation)

    visibleTo(navigation, as('developer'))

    expect(JSON.stringify(navigation)).toBe(before)
  })
})

describe('featuresIn', () => {
  it('lists every FEAT the menu points at', () => {
    const features = featuresIn(navigation)

    // Every entry carries one, so a new menu item cannot be added without
    // saying which feature it belongs to.
    expect(features.every((feature) => /^FEAT-\d{3}$/.test(feature))).toBe(true)
    expect(features).toContain('FEAT-009')
    expect(features).toContain('FEAT-023')
    expect(features).toContain('FEAT-017')
  })

  it('collapses the duplicates', () => {
    // Update Query Mgmt appears under both Admin and DBA, and Database Objects
    // has five entries against one feature.
    expect(new Set(featuresIn(navigation)).size).toBe(featuresIn(navigation).length)
  })
})

/*
 * Two entries pointing at one route both match `route.name === child.route`, so
 * both render as the current page and an operator cannot tell which one they
 * clicked. Three release entries did exactly that.
 */
describe('routes are not shared', () => {
  it('gives every routed entry a route of its own', () => {
    const seen = new Map<string, string>()
    const check = (label: string, route: string | undefined) => {
      if (route === undefined) return
      const already = seen.get(route)
      expect(already, `${label} and ${already} both route to ${route}`).toBeUndefined()
      seen.set(route, label)
    }

    for (const entry of navigation) {
      if (isGroup(entry)) {
        for (const child of entry.children) check(child.label, child.route)
        continue
      }
      check(entry.label, entry.route)
    }
  })
})

/*
 * What this deployment switched on, which is not what this operator may do.
 *
 * Avior runs without a write credential or a deploy credential, and every
 * endpoint on those paths then answers saying so. Staging came up that way and
 * the rail offered five entries whose entire content was a red line explaining
 * that they were switched off — the same lie the permission gate above exists to
 * prevent, arriving from the other direction.
 */
describe('what the deployment configured', () => {
  const dbaOn = (features: string[]) => ({
    roles: ['dba'],
    permissions: [...new Set(permissionsOf['dba'])],
    features,
  })

  it('hides the release group where no release target is configured', () => {
    const shown = labels(visibleTo(navigation, dbaOn(['write'])))

    expect(shown).not.toContain('Release')
    // And the rest of the rail is untouched: this is one path being absent, not
    // the operator losing anything.
    expect(shown).toContain('Query')
  })

  it('hides the write entries where no writer credential is configured', () => {
    const shown = labels(visibleTo(navigation, dbaOn(['release'])))

    expect(shown).not.toContain('Update Query')
    expect(shown).not.toContain('Update Requests')
    expect(shown).toContain('Release')
  })

  // A group whose children are all gone disappears with them rather than
  // becoming an expander that opens onto nothing.
  it('drops a group left empty by the deployment', () => {
    const entries = visibleTo(navigation, dbaOn([]))

    expect(labels(entries)).not.toContain('Release')
    expect(labels(entries)).toContain('Query')
  })

  /*
   * And the gate that keeps this true.
   *
   * A page added to either path without saying which feature it needs is
   * offered on a deployment that cannot serve it, and nothing else would notice
   * — the entry looks correct, it just opens onto a notice. The route names
   * carry the answer, so this reads them rather than trusting a second list.
   */
  it('every write and release page says which feature it needs', () => {
    const leaves = navigation.flatMap((entry) => (isGroup(entry) ? entry.children : [entry]))

    // Which capability, not merely that there is one: a page under Release that
    // needed the write target and said so would pass a check that only asked
    // whether the field was filled in.
    //
    // release-provisioning is the one release-prefixed page that does not need
    // a release target. It is grouped under Release because that is where
    // somebody looks for it, on the way to applying one — and it needs the
    // provisioning credential, which is a different thing a deployment can be
    // without. FEAT-059.
    const expected: Record<string, string> = {
      'release-provisioning': 'provision',
    }

    const unmarked = leaves.filter((leaf) => {
      const route = leaf.route ?? ''
      const wanted = expected[route]
      if (wanted !== undefined) return leaf.requires !== wanted
      if (route.startsWith('write-')) return leaf.requires !== 'write'
      if (route.startsWith('release-')) return leaf.requires !== 'release'
      return false
    })

    expect(unmarked.map((leaf) => leaf.label)).toEqual([])
  })

  // The gate is worth nothing if it cannot tell the two apart.
  it('the gate notices a page that forgot', () => {
    const forgot: NavEntry[] = [
      { label: 'New release page', icon: 'x', feature: 'FEAT-000', route: 'release-something' },
    ]
    const shown = labels(visibleTo(forgot, { roles: [], permissions: [], features: [] }))

    // Without `requires` it is offered on a deployment that has no release
    // target, which is exactly what the rule above is there to stop.
    expect(shown).toEqual(['New release page'])
  })
})
