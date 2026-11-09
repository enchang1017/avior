import type { components } from '@/gen/api'

/**
 * The roles, in one place, checked against the contract at build time.
 *
 * Four pages need this list — the account table, the database grant matrix, the
 * column unlock picker and the role guide — and each of them held its own copy.
 * Four copies of a fact that must agree is the drift this repository exists to
 * make impossible: adding a role meant remembering four files, and forgetting
 * one produced a page that silently could not express the new role rather than
 * a failure anybody would see.
 *
 * `Role` comes from the generated client, so the server's list is the authority
 * and the assertions below fail `vue-tsc` the moment this file disagrees with
 * it.
 */
export type Role = components['schemas']['CurrentUser']['roles'][number]

/** Every role, in the order an administrator reads them: least reach first. */
export const allRoles = [
  'developer',
  'dba',
  'release_approver',
  'update_approver',
  'administrator',
] as const

/**
 * The roles an administrator may grant on the account page.
 *
 * Administrator is absent, and that is FEAT-004's rule rather than an
 * oversight: the server refuses to grant it, and offering a control that is
 * certain to be refused is worse than not offering one.
 */
export const assignableRoles = allRoles.filter((role) => role !== 'administrator')

export const roleLabels: Record<Role, string> = {
  developer: 'Developer',
  dba: 'DBA',
  release_approver: 'Release approver',
  update_approver: 'Update approver',
  administrator: 'Administrator',
}

/** The label, or the raw name for a role this build does not know. */
export function roleLabel(role: string): string {
  return roleLabels[role as Role] ?? role
}

// Compile-time coverage, both ways. A role the server added and this file has
// not lists in Missing; one this file invented lists in Extra. Either makes the
// assignment below fail to typecheck, which is the whole point of the file.
type Missing = Exclude<Role, (typeof allRoles)[number]>
type Extra = Exclude<(typeof allRoles)[number], Role>

const everyRoleIsListed: Missing extends never ? true : Missing = true
const nothingInventedHere: Extra extends never ? true : Extra = true

void everyRoleIsListed
void nothingInventedHere
