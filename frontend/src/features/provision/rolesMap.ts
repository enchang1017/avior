import type { components } from '@/gen/api'

type Role = components['schemas']['ProvisionedRole']

/**
 * FEAT-076 AC-17. The roles about one database, laid out in three columns:
 * the database, its owner and group roles, and the logins. Pure, so what the
 * page draws — which card says what, and which line joins which — is decided
 * here and read by a test.
 */

export const databaseCard = '#database'

export type MapCard = {
  /** The role's name, or `databaseCard`. */
  id: string
  name: string
  kind: Role['kind'] | 'database' | 'more'
  /** For a "+N logins" card, the group whose dialog lists them. */
  group?: string
  canConnect: boolean
  /** The second line: what it owns, or whether it may connect and who it carries. */
  status: string
  /** Drawn in the warning tone: it may not connect. */
  warn: boolean
}

/** A line from a card in one column to a card in a column to its right. */
export type MapLink = { from: string; to: string; dashed: boolean }

export type RolesMap = {
  database: MapCard
  roles: MapCard[]
  logins: MapCard[]
  links: MapLink[]
}

type Reach = { canConnect: boolean; owned: number; privileged: boolean }

function reach(role: Role, database: string): Reach {
  const grant = role.grants.find((held) => held.database === database)
  if (grant === undefined) return { canConnect: false, owned: 0, privileged: false }
  return {
    // A grant without the flag came from a list naming only databases the
    // role may connect to.
    canConnect: grant.canConnect ?? true,
    owned: grant.owned ?? 0,
    privileged: grant.select + grant.insert + grant.update + grant.delete > 0,
  }
}

const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? '' : 's'}`

function status(role: Role, held: Reach): string {
  if (role.kind === 'owner')
    return held.owned > 0 ? `owns ${plural(held.owned, 'table')}` : '✓ connect'
  if (role.kind === 'login') return held.canConnect ? '✓ connect' : '⚠ cannot connect'
  const carried = role.members.length === 0 ? 'no login' : plural(role.members.length, 'member')
  return `${held.canConnect ? '✓ connect' : '⚠ cannot connect'} · ${carried}`
}

const byName = (a: Role, b: Role) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0)

/**
 * How many of a group's logins get a card of their own. Past it the rest are
 * one "+N logins" card that opens the group, whose dialog lists every member;
 * the logins column scrolls either way.
 */
export const loginCap = 6

export const moreCard = (group: string) => `+${group}`

/** The logins past the cap, by the first group on the map each belongs to. */
function overflow(logins: readonly Role[], inMiddle: Set<string>): Map<string, string[]> {
  const byGroup = new Map<string, string[]>()
  for (const login of logins) {
    const group = login.memberOf.find((name) => inMiddle.has(name))
    if (group !== undefined) byGroup.set(group, [...(byGroup.get(group) ?? []), login.name])
  }
  const hidden = new Map<string, string[]>()
  for (const [group, names] of byGroup) {
    if (names.length > loginCap + 1) hidden.set(group, names.slice(loginCap))
  }
  return hidden
}

export function rolesMap(roles: readonly Role[], database: string): RolesMap {
  const cards = new Map<string, MapCard>()
  const reaches = new Map<string, Reach>()
  for (const role of roles) {
    const held = reach(role, database)
    reaches.set(role.name, held)
    cards.set(role.name, {
      id: role.name,
      name: role.name,
      kind: role.kind,
      canConnect: held.canConnect,
      status: status(role, held),
      warn: !held.canConnect,
    })
  }
  const middle = [...roles]
    .filter((role) => role.kind !== 'login')
    .sort((a, b) => (a.kind === b.kind ? byName(a, b) : a.kind === 'owner' ? -1 : 1))
  const inMiddle = new Set(middle.map((role) => role.name))
  const logins = [...roles].filter((role) => role.kind === 'login').sort(byName)
  const hidden = overflow(logins, inMiddle)
  const unseen = new Set([...hidden.values()].flat())
  const right = logins.filter((login) => !unseen.has(login.name))

  const links: MapLink[] = []
  for (const role of middle) {
    const held = reaches.get(role.name)
    if (held === undefined) continue
    if (role.kind === 'owner' || held.canConnect) {
      links.push({ from: databaseCard, to: role.name, dashed: false })
    } else if (held.privileged) {
      links.push({ from: databaseCard, to: role.name, dashed: true })
    }
  }
  for (const login of right) {
    const groups = login.memberOf.filter((group) => inMiddle.has(group))
    for (const group of groups) links.push({ from: group, to: login.name, dashed: false })
    if (groups.length > 0) continue
    const held = reaches.get(login.name)
    if (held?.canConnect === true) links.push({ from: databaseCard, to: login.name, dashed: false })
    else if (held?.privileged === true)
      links.push({ from: databaseCard, to: login.name, dashed: true })
  }

  const more: MapCard[] = [...hidden].map(([group, names]) => {
    links.push({ from: group, to: moreCard(group), dashed: false })
    return {
      id: moreCard(group),
      name: `+${names.length} logins`,
      kind: 'more',
      group,
      canConnect: true,
      status: `members of ${group}`,
      warn: false,
    }
  })
  const pick = (list: Role[]) => list.flatMap((role) => cards.get(role.name) ?? [])
  return {
    database: {
      id: databaseCard,
      name: database,
      kind: 'database',
      canConnect: true,
      status: '',
      warn: false,
    },
    roles: pick(middle),
    logins: [...pick(right), ...more],
    links,
  }
}

type Placed = { key: string; from: string; x1: number; y1: number; x2: number }

/**
 * Where each line turns, so lines leaving different cards never share a
 * vertical run. Lines leaving from the same edge of the page share a gap; in
 * it, each source card gets a lane of its own, ordered top to bottom as the
 * cards are, spread evenly across the gap.
 */
export function laneTurns(lines: readonly Placed[]): Map<string, number> {
  const gaps = new Map<number, Placed[]>()
  for (const line of lines) gaps.set(line.x1, [...(gaps.get(line.x1) ?? []), line])
  const turns = new Map<string, number>()
  for (const [left, members] of gaps) {
    const right = Math.min(...members.map((line) => line.x2))
    const top = new Map<string, number>()
    for (const line of members)
      top.set(line.from, Math.min(top.get(line.from) ?? Infinity, line.y1))
    const sources = [...top.keys()].sort((a, b) => (top.get(a) ?? 0) - (top.get(b) ?? 0))
    const step = (right - left) / (sources.length + 1)
    for (const line of members) {
      turns.set(line.key, Math.round(left + step * (sources.indexOf(line.from) + 1)))
    }
  }
  return turns
}

/** A line's emphasis while one card is pointed at: its own lines lit, the rest faded. */
export function emphasis(link: MapLink, active: string | null): 'lit' | 'dim' | undefined {
  if (active === null) return undefined
  return link.from === active || link.to === active ? 'lit' : 'dim'
}
