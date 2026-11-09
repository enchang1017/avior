import { expect, request, test } from '@playwright/test'
import type { APIRequestContext, Locator, Page } from '@playwright/test'

// The e2e layer answers one question: do the deployables work together. Each
// case below spans the browser, the Go service, Avior's own database and the
// governed PostgreSQL database on the same server.
//
// Everything else belongs lower down. A case that only needs the backend is an
// integration case; a case that only needs a rule is a domain case. See
// docs/adr/0002-test-strategy.md.

// The local stack authenticates on a username alone. The credential paths are
// the integration suite's job, where the real verifier runs.
//
// PostgreSQL folds unquoted identifiers to lower case, so every object name here
// is the name the server actually holds, not a stylistic choice.
const fixture = {
  username: 'developer',
  database: 'aviorfixture',
}

async function signIn(page: Page): Promise<void> {
  await signInAs(page, fixture.username)
}

async function signInAs(page: Page, username: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(username)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(
    page.locator('.rail').getByRole('link', { name: 'Query', exact: true }),
  ).toHaveAttribute('aria-current', 'page')
}

// An approver never lands on the query console, so waiting for its heading
// would time out. They land wherever the rail's first entry takes them.
async function signInWithoutQuery(page: Page, username: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(username)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.locator('.rail')).toBeVisible()
}

async function apiAs(baseURL: string, username: string): Promise<APIRequestContext> {
  const api = await request.newContext({ baseURL })
  const login = await api.post('/api/v1/auth/login', { data: { username } })
  expect(login.ok(), `sign in as ${username}: ${login.status()}`).toBe(true)
  return api
}

/*
 * A write that ran and was put back, made through the API for the case that
 * reads it. The backup page lists every capture on the server, so a case that
 * relied on another case's write passed on a stack with history and failed on
 * a fresh one (R10). The undo keeps a backup of its own, so this leaves two
 * rows: the one put back and the rollback's.
 */
async function undoneBackup(baseURL: string, marker: string): Promise<void> {
  const developer = await apiAs(baseURL, fixture.username)
  const approver = await apiAs(baseURL, 'writeapprover')
  try {
    const asked = await developer.post('/api/v1/writes', {
      data: {
        database: fixture.database,
        statement: "UPDATE public.bet SET status = 'Void' WHERE bet_id = 10",
        reason: marker,
      },
    })
    expect(asked.status(), await asked.text()).toBe(201)
    const { id } = (await asked.json()) as { id: string }

    const approved = await approver.post(`/api/v1/writes/${id}/decision`, {
      data: { decision: 'approve' },
    })
    expect(approved.ok(), await approved.text()).toBe(true)

    const undone = await approver.post(`/api/v1/writes/${id}/rollback`, {
      data: { reason: `${marker} undone` },
    })
    expect(undone.ok(), await undone.text()).toBe(true)
  } finally {
    await developer.dispose()
    await approver.dispose()
  }
}

/*
 * Gets the fixture database open, rather than clicking it once and hoping.
 *
 * The node is a toggle: clicking an open database closes it. Under the full
 * suite the tree re-renders as the table list arrives, and a click that lands
 * across that render can leave the branch shut with the test waiting for
 * children that will never come — which is a flake about the helper rather than
 * about the page. Stating the condition and retrying to reach it is what the
 * helper is actually for.
 */
async function openDatabase(page: Page): Promise<void> {
  const node = page.locator('.node.database', { hasText: fixture.database })
  await expect(node).toBeVisible()

  await expect(async () => {
    const open = ((await node.getAttribute('class')) ?? '').includes('open')
    if (!open) await node.click()
    await expect(page.locator('.tables .node').first()).toBeVisible({ timeout: 2_000 })
  }).toPass({ timeout: 20_000 })
}

/**
 * Picks a value from one of the app's own dropdowns.
 *
 * Not `selectOption`: these are not native `select` elements. A native one
 * renders its list with the operating system's own widget, which is a different
 * font and a jarring highlight in the middle of this page, so the app draws its
 * own — and a test drives it the way a person does.
 */
async function choose(page: Page, control: string, label: string | RegExp): Promise<void> {
  await page.locator(control).click()
  await page.locator('.option').filter({ hasText: label }).first().click()
}

/**
 * A rail entry by the name it answers to. The rail folds to icons, so an entry
 * is found by its accessible name — the label, visually hidden, beside a
 * tooltip that repeats it — rather than by text on screen.
 */
function area(rail: Locator, name: string): Locator {
  return rail
    .getByRole('link', { name, exact: true })
    .or(rail.getByRole('button', { name, exact: true }))
}

async function openTable(page: Page, name: string): Promise<void> {
  await openDatabase(page)
  await page.locator('.tables .node', { hasText: name }).first().click()
  await expect(page.locator('.column-list li').first()).toBeVisible()
}

test('an operator picks columns, inserts a statement, and reads rows', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')

  // The readable columns start selected; the denied one is offered and refused.
  await expect(page.getByText('4 of 5 columns')).toBeVisible()

  // The banner names the whole target before anything runs. Ready rather than
  // armed: this stack runs AVIOR_ENV=development, and amber is production's.
  await expect(page.locator('.risk.ready')).toContainText(fixture.database)
  await expect(page.locator('.risk.ready')).toContainText('public.member')
  await expect(page.locator('.risk .env-name')).toHaveText('development')

  // The deployment's own answers, not the bundle's. Both were constants in the
  // view until FEAT-012's groundwork, and the instance crumb here is the host
  // from AVIOR_TARGET_DSN -- the seed's 'local-postgres' is overwritten by what
  // the backend registers on startup, so a stale name would show up right here.
  await expect(page.locator('.crumb.instance')).toHaveText('postgres')

  await page.getByRole('button', { name: 'Insert' }).click()

  // Generated server-side, so it always names the columns and always bounds the
  // result: the two things the guard would otherwise refuse it for.
  const editor = page.locator('.cm-content')
  await expect(editor).toContainText('LIMIT 100')
  await expect(editor).toContainText('member_id')
  await expect(editor).not.toContainText('*')
  await expect(editor).not.toContainText('id_card_number')

  await page.getByRole('button', { name: 'Execute' }).click()

  await expect(page.getByRole('columnheader', { name: /member_id/ })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'chen', exact: true })).toBeVisible()
  // The audit id has to reach the operator: it is what a later investigation
  // quotes back.
  await expect(page.locator('.result-pane .readout')).toContainText(/[0-9a-f-]{36}/)
})

/*
 * Picking a table writes the statement for it.
 *
 * Every readable column starts ticked, so the statement the operator wanted is
 * the one the tick boxes already describe, and making them press Insert to get
 * it was a step between them and what they came here to do.
 */
test('picking a table writes the statement, unquoted and without the denied column', async ({
  page,
}) => {
  await signIn(page)
  await openTable(page, 'member')

  const editor = page.locator('.cm-content')
  await expect(editor).toContainText('FROM public.member')

  // Quotes only where leaving them off would read something else. An operator
  // reads this before running it against production, and every ordinary name
  // wearing a pair of quotes is noise around a word.
  await expect(editor).toContainText('member_id, login_name')
  await expect(editor).not.toContainText('"member_id"')

  // The denied column is not in a statement the guard would then refuse.
  await expect(editor).not.toContainText('id_card_number')
})

/*
 * Ordering is the clause an operator adds by hand more than any other, and what
 * they almost always mean by it is "newest first" — so the dates sit at the top
 * of the list rather than being hunted for among forty columns.
 */
test('ordering is offered dates first, and rewrites the statement', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')

  await page.locator('#order-column').click()

  // The date column, ahead of the three ordinary ones. The first entry is the
  // way back to no ordering at all.
  await expect(page.locator('.option').nth(1)).toHaveText(/register_time.*date/)

  // A denied column is not offered: ORDER BY reads it, so the server refuses
  // it, and suggesting one would be suggesting a statement that cannot run.
  await expect(page.locator('.option').filter({ hasText: 'id_card_number' })).toHaveCount(0)

  await page.locator('.option').filter({ hasText: 'register_time' }).first().click()
  await expect(page.locator('.cm-content')).toContainText('ORDER BY register_time DESC')

  // And the statement it wrote is one the guard accepts.
  await page.keyboard.press('ControlOrMeta+Enter')
  await expect(page.getByRole('columnheader', { name: /register_time/ })).toBeVisible()
})

// Completion comes from the catalogue, so a name it offers is a name that
// exists. Worth an end-to-end case because it spans the schema endpoint, the
// editor's configuration and CodeMirror itself, and none proves it alone.
test('typing a column offers the ones this table has', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')

  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('SELECT curr')

  const options = page.locator('.cm-tooltip-autocomplete li')
  await expect(options.first()).toHaveText('currency_code')
})

/*
 * One letter matches eighty-five keywords, and producing that list blocked the
 * main thread for 375ms — long enough to swallow the next keystroke, which is
 * what an operator reported as the editor freezing. Measured the same in a
 * production build, so it was the work rather than the bundling.
 *
 * Two letters is where a list starts being an answer rather than an alphabet.
 */
test('a single letter opens no list, so typing is never interrupted', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')

  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('SELECT c')

  // Long enough for the typing delay to have elapsed twice over.
  await page.waitForTimeout(500)
  await expect(page.locator('.cm-tooltip-autocomplete')).toBeHidden()

  // And the second letter is what asks for it.
  await page.keyboard.type('u')
  await expect(page.locator('.cm-tooltip-autocomplete li').first()).toBeVisible()
})

test('a denied column is shown, marked, and cannot be selected', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')

  const denied = page.locator('.column-list li', { hasText: 'id_card_number' })
  await expect(denied).toContainText('denied')
  await expect(denied.locator('input[type="checkbox"]')).toBeDisabled()
})

// Only the plan phase reports several rules at once: a refusal raised while
// parsing stops at the first one. So this statement has to get past the parser
// and then break two plan rules — it reads the column denied to the developer
// role, and the fan-out puts its estimated cost over AVIOR_MAX_SUBTREE_COST.
test('a refused statement shows every rule that fired', async ({ page }) => {
  await signIn(page)
  await openDatabase(page)

  const wide = [
    'SELECT m.id_card_number, b1.stake FROM public.member m',
    'CROSS JOIN public.bet b1 CROSS JOIN public.bet b2 CROSS JOIN public.bet b3',
    'CROSS JOIN public.bet b4 CROSS JOIN public.bet b5 CROSS JOIN public.bet b6',
    'CROSS JOIN public.bet b7 CROSS JOIN public.bet b8',
  ].join(' ')

  await page.locator('.cm-content').fill(wide)
  await page.getByRole('button', { name: 'Execute' }).click()

  const verdict = page.locator('section.verdict')
  await expect(verdict).toContainText('Refused')

  // Both, not just the first. An operator shown one rule at a time fixes one
  // problem per submission.
  const rules = verdict.locator('.rule')
  await expect(rules.filter({ hasText: 'column_denied' })).toHaveCount(1)
  await expect(rules.filter({ hasText: 'cost_limit_exceeded' })).toHaveCount(1)

  // The plan, not the statement text, is what named the column.
  await expect(verdict).toContainText('public.member.id_card_number')

  // No result grid: a refused statement never ran.
  await expect(page.getByRole('columnheader', { name: /id_card_number/ })).toBeHidden()
})

// A guard refusal has to reach the operator with the rule that fired, so they
// can act on it rather than guessing. statement_not_read_only carries the
// concern this case was written for: a submission must not be able to change
// production.
test('a statement that is not read-only is refused with that rule', async ({ page }) => {
  await signIn(page)
  await openDatabase(page)

  await page
    .locator('.cm-content')
    .fill("UPDATE public.member SET login_name = 'mallory' WHERE member_id = 1")
  await page.getByRole('button', { name: 'Execute' }).click()

  const verdict = page.locator('section.verdict')
  await expect(verdict).toContainText('Refused')

  const reason = verdict.getByRole('listitem').first()
  await expect(reason.locator('.rule')).toHaveText('statement_not_read_only')
  // The rule name is a label. Without the sentence beside it the operator learns
  // which rule fired and nothing about what to do next.
  await expect(reason.locator('span:not(.rule)')).not.toBeEmpty()
})

test('the result grid filters the rows it was given', async ({ page }) => {
  await signIn(page)
  await openTable(page, 'member')
  await page.getByRole('button', { name: 'Insert' }).click()
  await page.getByRole('button', { name: 'Execute' }).click()
  await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()

  await page.getByPlaceholder('Search results').fill('bob')

  await expect(page.locator('.result')).toContainText('1 of 3')
  await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeHidden()
})

// The shortcut is printed on the button, which is the only reason anyone finds
// it, so it has to actually work.
test('the keyboard shortcut runs the statement', async ({ page }) => {
  await signIn(page)
  // No Insert: picking the table writes the statement. Pressing it again would
  // put a second request in flight, and the shortcut does nothing while the
  // statement is being rewritten.
  await openTable(page, 'bet')
  await expect(page.locator('.cm-content')).toContainText('LIMIT 100')

  await page.keyboard.press('ControlOrMeta+Enter')

  await expect(page.getByRole('columnheader', { name: /bet_id/ })).toBeVisible()
})

// The preference has to survive a reload, or an operator sets it every morning.
test('the theme can be switched and is remembered', async ({ page }) => {
  await signIn(page)

  const root = page.locator('html')
  const toggle = page.getByRole('button', { name: /^Theme:/ })

  // Night by default, whatever the system says; light is a choice that stays.
  await expect(toggle).toHaveAccessibleName('Theme: Dark')
  await expect(root).toHaveAttribute('data-theme', 'dark')

  await toggle.click()
  await expect(root).toHaveAttribute('data-theme', 'light')
  // The editor follows the page rather than staying dark under a day shell.
  await expect(page.locator('.cm-gutters')).toHaveCSS('background-color', 'rgb(238, 240, 234)')

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.getByRole('button', { name: /^Theme:/ })).toHaveAccessibleName('Theme: Light')
})

// The rail carries the predecessor's menu, in its order, and lets a group open.
test('the rail carries the predecessor menu, with groups that expand', async ({ page }) => {
  await signIn(page)

  const rail = page.locator('.rail')
  for (const label of ['Query', 'Update Query', 'Database Objects', 'Release', 'Personal Log']) {
    await expect(area(rail, label)).toBeVisible()
  }

  // The developer fixture holds no admin or dba role, so those groups are absent.
  await expect(area(rail, 'Admin')).toBeHidden()
  await expect(area(rail, 'DBA')).toBeHidden()

  // Unbuilt entries are shown to nobody: an entry that cannot be clicked is not
  // a menu item but a promise, so they are absent rather than greyed.
  for (const label of ['Export Data', 'Restore DB', 'Application Setting']) {
    await expect(area(rail, label)).toBeHidden()
  }

  // Collapsed until asked.
  await expect(area(rail, 'Stored Procedure')).toBeHidden()
  await rail.getByRole('button', { name: /Database Objects/ }).click()

  for (const type of ['View', 'Stored Procedure', 'User Defined Type', 'Function', 'Index']) {
    await expect(area(rail, type)).toBeVisible()
  }

  // Nothing an operator can see is unreachable, so nothing renders as pending.
  await expect(rail.locator('.rail-item.pending')).toHaveCount(0)
})

/*
 * The rail is gated on permissions, not only on roles, and this is what proves
 * the two agree with the server.
 *
 * A release approver holds release.approve and nothing else, so Query and
 * Database Objects would answer 403. Offering them is a menu that lies about
 * what it opens, which is what an operator reported: the entry was there and
 * clicking it said "This needs the query.inspect permission."
 *
 * The unit test over visibleTo states the same rule against a hand-written
 * caller. Only this case signs in as a real seeded operator, so only this one
 * fails if the frontend's idea of what a role holds drifts from the server's.
 */
test('an approver is offered nothing that would refuse them', async ({ page }) => {
  await signInWithoutQuery(page, 'approver')

  const rail = page.locator('.rail')
  await expect(area(rail, 'Release')).toBeVisible()
  await expect(area(rail, 'Query')).toBeHidden()
  await expect(area(rail, 'Database Objects')).toBeHidden()
  await expect(area(rail, 'Personal Log')).toBeHidden()
})

// The other half of the same rule. An administrator is shown what exists and
// nothing else: an entry that cannot be clicked is a promise, not a menu item,
// and what is left to build is the feature registry's to say.
test('an administrator is offered nothing that is not built', async ({ page }) => {
  await signInAs(page, 'administrator')

  const rail = page.locator('.rail')
  await expect(area(rail, 'Admin')).toBeVisible()
  for (const label of ['Export Data', 'Restore DB', 'Healthy Check', 'Application Setting']) {
    await expect(area(rail, label)).toBeHidden()
  }
  await rail.getByRole('button', { name: /Admin/ }).click()
  await expect(area(rail, 'Account Management')).toBeVisible()
  for (const label of ['DB Resource Mgmt', 'Restart']) {
    await expect(area(rail, label)).toBeHidden()
  }
})

// FEAT-023. Worth an end-to-end case because it spans the browser, the API and
// the governed database, and none of the three can prove the walk alone.
test('reading a stored procedure definition', async ({ page }) => {
  await signIn(page)

  await page
    .locator('.rail')
    .getByRole('button', { name: /Database Objects/ })
    .click()
  await page.locator('.rail').getByRole('link', { name: 'Stored Procedure' }).click()

  await expect(page.getByText('Stored Procedure definition')).toBeVisible()

  await choose(page, '[id^=database-]', fixture.database)
  await page.getByRole('button', { name: 'public sp_member_bets' }).click()

  // The name is the heading: the type is already on the rail and the top bar.
  await expect(page.getByRole('heading', { name: 'public.sp_member_bets' })).toBeVisible()

  // PostgreSQL renders a routine definition as CREATE OR REPLACE, so the header
  // alone would pass on a truncated read. The body has to be there too.
  await expect(page.locator('.script').first()).toContainText('PROCEDURE')
  await expect(page.locator('.script').first()).toContainText('public.bet')

  // The grants are shown apart from the body, not concatenated onto it the way
  // the predecessor does.
  await expect(page.getByRole('heading', { name: 'Permissions' })).toBeVisible()
  await expect(page.locator('.grants .script')).toContainText('avior_reader')
  await expect(page.locator('.script').first()).not.toContainText('GRANT')
})

// The five entries share one component instance, so moving between them is the
// case that breaks: the heading followed the route while the list did not.
test('moving between object types replaces the list, not just the heading', async ({ page }) => {
  await signIn(page)

  const rail = page.locator('.rail')
  await rail.getByRole('button', { name: /Database Objects/ }).click()
  await rail.getByRole('link', { name: 'Stored Procedure' }).click()
  await choose(page, '[id^=database-]', fixture.database)

  // The label carries the type in its `for`, so it is the thing that was wrong:
  // the heading followed the route and this did not.
  const listLabel = page.locator('.tree label[for^="filter-"]')

  await expect(listLabel).toContainText('Stored Procedure')
  await expect(page.getByRole('button', { name: 'public sp_member_bets' })).toBeVisible()

  await rail.getByRole('link', { name: 'Function' }).click()

  await expect(page.getByText('Function definition')).toBeVisible()
  await expect(listLabel).toContainText('Function')
  await expect(page.getByRole('button', { name: 'public fn_member_stake' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'public sp_member_bets' })).toBeHidden()

  // Index lists tables, which is the predecessor's label for that entry.
  await rail.getByRole('link', { name: 'Index' }).click()

  await expect(page.getByText('Table definition')).toBeVisible()
  await expect(listLabel).toContainText('Table')
  await expect(page.getByRole('button', { name: 'public bet', exact: true })).toBeVisible()
})

/*
 * FEAT-054. Four sections rather than one script, because a table is four
 * independent facts — and the predecessor had no such page at all: its "Table" menu
 * was the index list, so an operator wanting to know what a column held read a
 * CREATE INDEX statement and inferred.
 *
 * Worth an end-to-end case because it spans the catalogue, the endpoint and
 * the page, and none of the three proves the walk alone.
 */
test('a table reads as its columns, indexes, permissions and description', async ({ page }) => {
  await signInAs(page, fixture.username)
  await page.goto('/objects/table')

  await choose(page, '#table-database', fixture.database)
  await page.locator('.list .row:has(.name:text-is("member"))').click()

  await expect(page.getByRole('heading', { name: /public\.member/ })).toBeVisible()

  // The description is the table's comment, which no other page shows.
  await expect(page.getByText('One row per registered player.')).toBeVisible()

  // The rendered type is the one the query console shows. Two names for one
  // type is two things to learn.
  await expect(page.getByRole('cell', { name: 'character(3)' })).toBeVisible()

  // A column's own comment, beside the column rather than in a separate list.
  await expect(page.getByText('National identity number.')).toBeVisible()

  // The index section carries the statement that recreates the primary key.
  await expect(page.getByText('Indexes')).toBeVisible()
  await expect(page.locator('.sections')).toContainText('member_pkey')
})

// AC-9: a view has columns, grants and a comment and no indexes. Refusing it
// would be refusing the answer over a distinction the operator did not make.
test('a view is answered too, and says it carries no index', async ({ page }) => {
  await signInAs(page, fixture.username)
  await page.goto('/objects/table')

  await choose(page, '#table-database', fixture.database)
  await page.locator('.list .row:has(.name:text-is("member_view"))').click()

  await expect(page.getByRole('heading', { name: /public\.member_view/ })).toBeVisible()
  await expect(page.locator('.sections')).toContainText('A view carries no index.')
})

/*
 * FEAT-004. Granting a role is deciding what somebody may do against a
 * production database, and `release_approver` does not say that on its own.
 *
 * End-to-end because this is the one thing the unit tests cannot reach: jsdom
 * ships `<dialog>` without `showModal`, so the modal behaviour the component is
 * written against — the top layer, the inert background, escape — is only real
 * in a browser.
 */
test('an administrator reads what a role opens before granting it', async ({ page }) => {
  await signInWithoutQuery(page, 'administrator')
  await page.goto('/admin/accounts')

  await page.getByRole('button', { name: 'What each role opens' }).click()

  const guide = page.locator('dialog')
  await expect(guide).toBeVisible()
  // The permissions are the server's, from the same table the middleware reads.
  await expect(guide).toContainText('query.execute')
  await expect(guide).toContainText('release.approve')
  // FEAT-009's role, and the sentence that says why it is not the release
  // approver's: an administrator granting one is deciding which judgement
  // somebody is being asked to make.
  await expect(guide).toContainText('write.approve')
  // The role the page offers no switch for, saying so rather than leaving the
  // reader to look for a control that is not there.
  await expect(guide.getByText('not granted here')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(guide).toBeHidden()
})

/*
 * FEAT-004. The predecessor let only a DBA hand out the DBA role. That is not ported,
 * and the page has to agree with the endpoint: an administrator holding no
 * other role grants a role, and it is still there after a reload.
 *
 * The subject is the seeded `dba` account because the suite runs fully in
 * parallel and this case writes: granting to `approver` or `developer` would
 * change what a rail case sees mid-assertion (R10). Nothing else signs in as
 * `dba`, and the grant is undone before the case ends.
 */
test('an administrator grants a role and it sticks', async ({ page }) => {
  await signInWithoutQuery(page, 'administrator')
  await page.goto('/admin/accounts')

  const approver = () =>
    page.locator('tbody tr', { hasText: /^dba/ }).first().locator('td.switch').nth(2)

  /*
   * Read the starting state rather than assuming it.
   *
   * This case writes, and a run interrupted between the grant and the
   * revocation leaves the role behind — after which a test that assumed the
   * seed's state could never pass again, on any machine, until somebody went
   * and deleted the row by hand. Toggling from wherever it is survives that.
   */
  await expect(approver().getByRole('button')).toBeEnabled()
  const before = (await approver().innerText()).trim()
  const after = before === 'Held' ? 'Grant' : 'Held'

  await approver().getByRole('button', { name: before }).click()
  await expect(approver().getByRole('button', { name: after })).toBeVisible()

  await page.reload()
  await expect(approver()).toContainText(after)

  // A test owns its data, and the next case to read this page has to find it
  // the way it was left.
  await approver().getByRole('button', { name: after }).click()
  await expect(approver().getByRole('button', { name: before })).toBeVisible()
})

/*
 * FEAT-057. Modal editing in the box that submits statements to production.
 *
 * End-to-end because every one of these is a claim about what a keystroke does,
 * and the only thing that can answer that is a browser holding a real
 * CodeMirror with a real keymap stack.
 */

// AC-1. Somebody who has never chosen finds an editor that takes text.
test('vim keybindings are off until they are asked for', async ({ page }) => {
  await signIn(page)

  await expect(page.locator('.modeline .mode')).toHaveText('')
  await page.locator('.cm-content').click()
  await page.keyboard.type('iSELECT')

  // With vim on, the leading `i` would enter insert mode instead of being text.
  await expect(page.locator('.cm-content')).toContainText('iSELECT')
})

// AC-2, AC-3, AC-4. The switch costs nothing and says where it put you.
test('turning vim on keeps the statement, names the mode, and is remembered', async ({ page }) => {
  await signIn(page)

  const editor = page.locator('.cm-content')
  await editor.click()
  await page.keyboard.type('SELECT 1;')

  await page.locator('.modeline button').click()

  // The statement survives: the extension is reconfigured, not rebuilt.
  await expect(editor).toContainText('SELECT 1;')
  await expect(page.locator('.modeline .mode')).toHaveText('-- NORMAL --')

  await page.reload()
  await expect(page.locator('.modeline .mode')).toHaveText('-- NORMAL --')
})

/*
 * AC-5. The failure this card exists to prevent.
 *
 * Somebody who does not use vim turns it on, cannot type, and has to get out
 * without knowing that `i` means anything. The control is outside the editor
 * and takes a click.
 */
test('the way out of vim does not need vim', async ({ page }) => {
  await signIn(page)
  await page.locator('.modeline button').click()

  const editor = page.locator('.cm-content')
  await editor.click()
  await page.keyboard.type('hello')
  // In normal mode those letters are commands, so none of them is text.
  await expect(editor).not.toContainText('hello')

  await page.locator('.modeline button').click()

  await expect(page.locator('.modeline .mode')).toHaveText('')
  await editor.click()
  await page.keyboard.type('SELECT 1;')
  await expect(editor).toContainText('SELECT 1;')
})

// AC-7. Escape belongs to whichever of the two is showing. Vim takes it to
// leave insert mode, and without this the operator dismissing a suggestion was
// dropped into normal mode with their next letters read as commands.
test('escape closes the completion list before it leaves insert mode', async ({ page }) => {
  await signIn(page)
  await page.locator('.modeline button').click()

  const editor = page.locator('.cm-content')
  await editor.click()
  await page.keyboard.press('i')
  await expect(page.locator('.modeline .mode')).toHaveText('-- INSERT --')

  // AC-8. The completion FEAT-008 built still runs while vim is on.
  await editor.pressSequentially('SEL')
  await expect(page.locator('.cm-tooltip-autocomplete')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(page.locator('.cm-tooltip-autocomplete')).toBeHidden()
  await expect(page.locator('.modeline .mode')).toHaveText('-- INSERT --')

  await page.keyboard.press('Escape')
  await expect(page.locator('.modeline .mode')).toHaveText('-- NORMAL --')
})

// AC-6. The shortcuts belong to the page. In normal mode the letters are
// commands, and ⌘↵ still has to reach the button it belongs to.
test('the page keeps its shortcuts while vim holds the keyboard', async ({ page }) => {
  await signIn(page)
  await openDatabase(page)
  await page.locator('.tables .node', { hasText: 'member' }).first().click()
  await expect(page.locator('.cm-content')).toContainText('FROM public.member')

  await page.locator('.modeline button').click()
  await page.locator('.cm-content').click()
  await expect(page.locator('.modeline .mode')).toHaveText('-- NORMAL --')

  await page.keyboard.press('ControlOrMeta+Enter')

  await expect(page.locator('.result')).toBeVisible()
})

/*
 * FEAT-058. The address bar is the way past the rail.
 *
 * The rail already hides what somebody cannot use, so this is about a typed
 * URL, a stale bookmark and a link somebody was sent. They land on the page
 * they would have clicked rather than on one that refuses them.
 */
test('a typed url for a page you cannot open lands you on one you can', async ({ page }) => {
  await signInWithoutQuery(page, 'approver')

  await page.goto('/admin/accounts')

  // An approver holds release.approve and nothing else, so the query console
  // would refuse them too — the landing page is their own rail's first entry.
  await expect(page).toHaveURL(/\/releases$/)
  await expect(page.getByRole('heading', { name: 'Waiting for a decision' })).toBeVisible()
})

/*
 * FEAT-009. The whole of the write path through the browser: a developer asks
 * for a row change, an approver reads it and lets it run, and a DBA sees what
 * the write kept.
 *
 * One case rather than four, because it is one thing that happens and the
 * halves cannot be checked apart: a request nobody approved proves nothing
 * about the capture, and a capture with no request in front of it proves
 * nothing about the page. The rules themselves are integration cases.
 */
test('a write request is asked for, approved, and leaves a backup behind', async ({ page }) => {
  const marker = `e2e ${Date.now()}`

  await signIn(page)
  await page.getByRole('link', { name: 'Update Query', exact: true }).click()

  // The composer is the query console's shape against a different verb: pick
  // the table and the skeleton is written, rather than typing it from memory.
  await openDatabase(page)
  await page.locator('.tables .node', { hasText: 'bet' }).first().click()
  await expect(page.locator('.cm-content')).toContainText('UPDATE public.bet')
  await expect(page.locator('.cm-content')).toContainText('SET')

  // Finished the way an operator finishes it, then checked before anybody is
  // asked to read it.
  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type("UPDATE public.bet SET status = 'Void' WHERE bet_id = 10")
  await page.getByRole('button', { name: 'Check' }).click()
  await expect(page.getByText('The guard accepts this.')).toBeVisible()

  // What would be kept, as facts rather than as a claim.
  await expect(page.getByText('What would be kept')).toBeVisible()
  await expect(page.locator('.backup')).toContainText('avior_backup.captured_row')

  /*
   * And the editor is still there.
   *
   * The answer panel grows by a table's worth of columns when it carries the
   * reverse statement, and the editor's `min-height: 0` — which it needs in
   * order to scroll rather than push the pane — also let it shrink to nothing.
   * Checking a statement made the statement disappear.
   */
  const editor = await page.locator('.editor-holder').boundingBox()
  expect(editor?.height ?? 0).toBeGreaterThan(100)

  await page.getByLabel('Why').fill(marker)
  await page.getByRole('button', { name: 'Ask for approval' }).click()
  await expect(page.getByRole('heading', { name: 'Write requests', exact: true })).toBeVisible()

  await signInWithoutQuery(page, 'writeapprover')
  // The group is collapsed until it is asked for, the way every group is.
  await page.locator('.rail').getByRole('button', { name: /DBA/ }).click()
  await page.getByRole('link', { name: 'Update Query Mgmt' }).click()
  await expect(page.getByRole('heading', { name: 'Write requests waiting' })).toBeVisible()

  await page.locator('[data-request]', { hasText: marker }).click()
  const reading = page.getByTestId('request-detail')
  await expect(reading.getByText(marker)).toBeVisible()
  await reading.getByRole('button', { name: 'Approve and run' }).click()

  // The reverse statement is generated on the approval and is what somebody
  // restoring would read.
  await expect(reading.getByText('To undo it')).toBeVisible()
  await expect(reading.getByText('avior_backup.captured_row', { exact: true })).toBeVisible()

  // The DBA's page shows what it kept, with no schedule that removes it — and
  // the statement that would put the rows back is one click away on the page
  // rather than behind a modal.
  await page.locator('.rail').getByRole('link', { name: 'Backup Tables' }).click()
  await expect(page.getByRole('heading', { name: 'Backups the write path has kept' })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'public.bet' }).first()).toBeVisible()

  const kept = page.locator('tr', { hasText: marker })
  await kept.getByRole('button', { name: 'Undo SQL' }).click()
  await expect(page.locator('tr.undo').first()).toContainText('avior_backup.captured_row')
  // The statement is highlighted rather than a bare <pre>: same CodeMirror the
  // editor uses, read-only.
  await expect(page.locator('tr.undo .sql-view').first()).toContainText('UPDATE public.bet')

  /*
   * And undoing it. The rows go back, and the rollback becomes a write request
   * of its own with a backup of its own — so this can be undone in turn, which
   * is the whole reason it is a request rather than a flag.
   */
  await kept.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Put these rows back' })).toBeVisible()
  await page.getByLabel('Why').fill('settled in error')
  await page.getByRole('button', { name: 'Put them back' }).click()

  /*
   * The row stays, marked, and stops offering the button.
   *
   * A rollback does not remove the capture it read: the rows are still there to
   * prune, and there is nothing left to put back. The list is read again rather
   * than filtered, because the rollback keeps a backup of its own and that is a
   * new row here — which is why dropping the row locally left the page telling
   * a story only a manual reload corrected.
   */
  const undone = page.locator('tr', { hasText: marker }).first()
  await expect(undone).toContainText('already put back')
  await expect(undone.getByRole('button', { name: 'Undo', exact: true })).toHaveCount(0)
  await expect(undone.getByRole('button', { name: 'Remove' })).toBeVisible()

  // And it survives a reload saying the same thing, rather than only looking
  // right until somebody refreshes.
  await page.reload()
  const back = page.locator('tr', { hasText: marker }).first()
  await expect(back).toContainText('already put back')
  await expect(back.getByRole('button', { name: 'Undo', exact: true })).toHaveCount(0)
})

/*
 * FEAT-009. Check is what makes the composer worth using: it answers whether
 * the statement would be accepted, and how many rows it would touch, before
 * anybody is asked to read it. A refusal found here costs a request nobody had
 * to look at.
 */
test('the composer answers a refusal before anybody is asked to approve it', async ({ page }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Update Query', exact: true }).click()

  await openDatabase(page)
  await page.locator('.tables .node', { hasText: 'bet' }).first().click()
  // The skeleton arrives from the server after the click. Selecting all before
  // it lands leaves the typed statement interleaved with it.
  await expect(page.locator('.cm-content')).toContainText('UPDATE public.bet')

  // The generated skeleton carries no predicate, which is exactly the mistake
  // this card exists to prevent — so the skeleton itself is refused until it is
  // finished.
  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type("UPDATE public.bet SET status = 'Void'")
  await page.getByRole('button', { name: 'Check' }).click()

  await expect(page.getByText('missing_where_clause')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Ask for approval' })).toBeDisabled()
})

/*
 * A link that looks like a button has to look like one all the way.
 *
 * End-to-end because it is a fact about the cascade, and jsdom resolves no
 * stylesheet: the underline came from the browser's own anchor default, which a
 * unit test would never have applied in the first place. It shipped once, so it
 * gets a case rather than a fixed rule and a hope.
 */
test('a button that navigates still renders as a button', async ({ page }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Update Requests', exact: true }).click()

  const ask = page.getByRole('link', { name: 'Ask for a change' })
  await expect(ask).toBeVisible()

  const style = await ask.evaluate((element) => {
    const computed = getComputedStyle(element)
    return { decoration: computed.textDecorationLine, cursor: computed.cursor }
  })
  expect(style.decoration).toBe('none')
  expect(style.cursor).toBe('pointer')

  // Still a link, which is the reason it is not a button: middle-click and
  // open-in-new-tab have to keep working.
  await expect(ask).toHaveAttribute('href', /\/writes\/new$/)
})

/*
 * FEAT-009 AC-5. A requester cannot decide their own request, and the page does
 * not offer them the button.
 *
 * The refusal was always there; what was not is the page acting on it. A DBA
 * holds write.approve, so they were shown Approve and run on a request they had
 * asked for themselves and answered 403 after the click — a control certain to
 * be refused, which is the thing the account page already refuses to draw.
 */
test('a requester is not offered the decision on their own request', async ({ page }) => {
  const marker = `own ${Date.now()}`

  // A DBA: they hold write.approve, which is what makes this reachable at all.
  await signInWithoutQuery(page, 'dba')
  await page.getByRole('link', { name: 'Update Query', exact: true }).click()

  await openDatabase(page)
  await page.locator('.tables .node', { hasText: 'bet' }).first().click()
  await expect(page.locator('.cm-content')).toContainText('UPDATE public.bet')
  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type("UPDATE public.bet SET status = 'Void' WHERE bet_id = 11")
  await page.getByRole('button', { name: 'Check' }).click()
  await expect(page.getByText('The guard accepts this.')).toBeVisible()
  await page.getByLabel('Why').fill(marker)
  await page.getByRole('button', { name: 'Ask for approval' }).click()

  await page.locator('[data-request]', { hasText: marker }).click()

  await expect(page.getByText('somebody else has to decide it')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Approve and run' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Refuse' })).toHaveCount(0)
  // What they can do instead, which is the useful half of saying no.
  await expect(page.getByRole('button', { name: 'Withdraw' })).toBeVisible()
})

/*
 * FEAT-009. Two layout facts that only a browser can answer, and that shipped
 * wrong: the header stopped at a margin with the pane showing through around
 * it, and the last column stopped sharing a width with the rows above it, so
 * the buttons walked off the right edge.
 *
 * The second was `display: flex` on a `<td>`, which takes the cell out of the
 * table's column algorithm. Nothing in a type checker or a unit test can see
 * either one.
 */
test('the backup page fills its pane and keeps its columns in line', async ({ page, baseURL }) => {
  await undoneBackup(baseURL ?? '', `e2e columns ${Date.now()}`)
  await signInWithoutQuery(page, 'dba')
  await page.locator('.rail').getByRole('button', { name: /DBA/ }).click()
  await page.locator('.rail').getByRole('link', { name: 'Backup Tables' }).click()
  await expect(page.getByRole('heading', { name: 'Backups the write path has kept' })).toBeVisible()

  // The header spans the pane rather than sitting inside a margin.
  const pane = await page.locator('.captures').boundingBox()
  const header = await page.locator('.captures > *').first().boundingBox()
  expect(Math.round(header?.x ?? -1)).toBe(Math.round(pane?.x ?? -2))
  expect(Math.round(header?.width ?? -1)).toBe(Math.round(pane?.width ?? -2))

  // And every row's actions start at the same place, whether the row carries
  // two buttons or three. The rows arrive after the heading, so they are
  // waited for; measured at once, an empty table is two lines that agree.
  const cells = page.locator('tbody tr td.actions-cell')
  await expect(cells.nth(1)).toBeAttached()
  const lefts = await cells.evaluateAll((cells) =>
    cells.map((cell) => Math.round(cell.getBoundingClientRect().left)),
  )
  expect(lefts.length).toBeGreaterThan(1)
  expect(new Set(lefts).size).toBe(1)
})

/*
 * FEAT-009 AC-30. An undo is not undone; the change is asked for again.
 *
 * It is mechanically possible and refused anyway: the one-click exemption rests
 * on a rollback only putting back what an approved write took away, and when
 * the thing being undone is itself the correction, what is left is re-applying
 * a change a second person removed with nobody agreeing to it.
 */
test('an undone change is asked for again rather than undone twice', async ({ page, baseURL }) => {
  const marker = `e2e ask again ${Date.now()}`
  await undoneBackup(baseURL ?? '', marker)
  await signInWithoutQuery(page, 'writeapprover')
  await page.locator('.rail').getByRole('button', { name: /DBA/ }).click()
  await page.locator('.rail').getByRole('link', { name: 'Backup Tables' }).click()
  await expect(page.getByRole('heading', { name: 'Backups the write path has kept' })).toBeVisible()

  // A row that was undone: no Undo, and the way back is a fresh request.
  const undone = page.locator('tr', { hasText: marker }).filter({ hasText: 'already put back' })
  await expect(undone).toBeVisible()
  await expect(undone.getByRole('button', { name: 'Undo', exact: true })).toHaveCount(0)

  await undone.getByRole('link', { name: 'Ask again' }).click()

  // The composer opens carrying the statement, so nobody reconstructs it — and
  // it is an ordinary request from here, with a reason and an approval.
  await expect(page).toHaveURL(/\/writes\/new\?from=/)
  await expect(page.locator('.cm-content')).toContainText('public.bet')
  await expect(page.getByRole('button', { name: 'Ask for approval' })).toBeDisabled()
})

/*
 * The date filter is the app's, not the operating system's.
 *
 * `<input type="datetime-local">` answers with a light panel in another font,
 * sized for a phone, under a dark toolbar it knows nothing about. VSelect exists
 * for the same reason and this is the louder case.
 */
test('picking a time uses the app own calendar', async ({ page }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Personal Log' }).click()
  await expect(page.getByRole('heading', { name: 'Your submissions' })).toBeVisible()

  // No native control left to hand the job over with.
  await expect(page.locator('input[type="datetime-local"]')).toHaveCount(0)

  await page.locator('#audit-from').click()
  const panel = page.getByRole('dialog', { name: 'Choose a date and time' })
  await expect(panel).toBeVisible()

  // Six weeks, always, so paging does not move the day under the pointer.
  await expect(panel.locator('.day')).toHaveCount(42)

  await panel.locator('.day.today').click()
  await expect(page.locator('#audit-from .shown')).not.toHaveClass(/empty/)

  // Escape closes it, the way every other overlay here does.
  await page.keyboard.press('Escape')
  await expect(panel).toBeHidden()
})

/*
 * Signing out has to leave.
 *
 * The store dropped the caller and stopped there, and the guard that sends
 * somebody to the login page only runs on a navigation — so the operator stayed
 * on the page they were on, signed out, reading what it had already rendered.
 * Nothing anywhere covered the button, which is why that survived.
 */
test('signing out lands on the login page and does not come back', async ({ page }) => {
  await signIn(page)

  await page.getByRole('button', { name: 'Your account' }).click()
  await page.getByRole('button', { name: 'Sign out' }).click()

  await expect(page).toHaveURL(/\/login/)
  await expect(page.getByLabel('Username')).toBeVisible()

  // And the session is actually gone: asking for a private page again returns
  // to the login form rather than rendering it from what the browser still held.
  await page.goto('/')
  await expect(page.getByLabel('Username')).toBeVisible()
})

/*
 * A statement belongs to whoever wrote it. The tabs were kept under one key for
 * the whole browser, so the next person to sign in was handed the last one's
 * statement, and their console asked for a schema they could not reach.
 */
test("one operator's statements never open for the next", async ({ page }) => {
  const refused: string[] = []
  page.on('response', (response) => {
    if (response.status() === 403) refused.push(response.url())
  })

  await signIn(page)
  await openDatabase(page)
  await page.locator('.cm-content').fill('SELECT 1 AS written_by_developer')
  await expect(page.locator('.editor-tab.active')).toContainText(fixture.database)

  await page.getByRole('button', { name: 'Your account' }).click()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByLabel('Username')).toBeVisible()
  expect(await page.evaluate(() => Object.keys(window.localStorage))).not.toContain(
    'avior.tabs.developer',
  )

  await signInWithoutQuery(page, 'administrator')
  await page.goto('/')
  await expect(page.locator('.editor-tab')).toHaveCount(1)
  await expect(page.locator('.editor-tab')).toContainText('Untitled')
  await expect(page.locator('.cm-content')).not.toContainText('written_by_developer')
  expect(refused).toEqual([])
})

test('a stored tab for a database the caller cannot reach is not restored', async ({ page }) => {
  await signIn(page)
  await openDatabase(page)
  await page.locator('.cm-content').fill('SELECT 1 AS before_the_grant_went')
  await expect(page.locator('.editor-tab.active')).toContainText(fixture.database)

  // The grant is withdrawn between two visits: the server lists no database.
  const asked: string[] = []
  await page.route('**/api/v1/schema/databases**', (route) => route.fulfill({ json: [] }))
  page.on('request', (request) => {
    if (request.url().includes('/schema/tables')) asked.push(request.url())
  })
  await page.reload()
  await expect(page.locator('.editor-tab')).toHaveCount(1)
  await expect(page.locator('.editor-tab')).toContainText('Untitled')
  await expect(page.locator('.cm-content')).not.toContainText('before_the_grant_went')
  expect(asked).toEqual([])
})

/*
 * The header used to spell out every role the operator held, which on a DBA was
 * four chips across the most expensive strip of the page — answering a question
 * the rail already answers by what it shows. What it did not say was which
 * deployment this is, and two Aviors open at once is the ordinary case: until
 * this, the only thing telling them apart was the address bar.
 */
test('the header names the deployment and keeps the rest behind a menu', async ({ page }) => {
  await signIn(page)

  const header = page.locator('header')
  await expect(header.getByText('DEVELOPMENT', { exact: true })).toBeVisible()

  // The roles are not on the toolbar any more, and nor is signing out. The
  // fixture operator is named after their role, so this asks for the chip
  // rather than for the word.
  await expect(header.locator('.chip')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeHidden()

  await page.getByRole('button', { name: 'Your account' }).click()
  const menu = page.getByRole('menu')
  await expect(menu.locator('.chip', { hasText: 'developer' })).toBeVisible()
  await expect(menu.getByRole('link', { name: 'Account' })).toBeVisible()
  await expect(menu.getByRole('link', { name: 'Connect your agent' })).toBeVisible()

  // The theme toggle stays outside it: the menu closes on any click inside, so
  // cycling three states from in there would mean opening it three times.
  await expect(header.getByRole('button', { name: /^Theme:/ })).toBeVisible()
})

// Both pages are reached from that menu and from nowhere else, so the menu is
// how they are opened here too.
test('the account pages open from the menu', async ({ page }) => {
  await signIn(page)

  await page.getByRole('button', { name: 'Your account' }).click()
  await page.getByRole('link', { name: 'Account' }).click()
  await expect(page.getByRole('heading', { name: 'Who you are here' })).toBeVisible()
  await expect(page.getByText('query.execute')).toBeVisible()

  await page.getByRole('button', { name: 'Your account' }).click()
  await page.getByRole('link', { name: 'Connect your agent' }).click()
  await expect(page.getByRole('heading', { name: 'Connect your agent', exact: true })).toBeVisible()
  // FEAT-060. The command points at this deployment's own endpoint, and the
  // token is a placeholder until one is minted here. Shown rather than run:
  // nothing on this page reaches into the operator's machine.
  const command = page.locator('.command').first()
  await expect(command).toContainText('claude mcp add --transport http avior')
  await expect(command).toContainText('/api/v1/mcp')
  await expect(command).toContainText('<YOUR_TOKEN>')

  // Minting here fills the token into the command, once.
  await page.getByLabel('Token name').fill(`e2e-agent-${Date.now()}`)
  await page.getByRole('button', { name: 'Create token' }).click()
  const secret = page.locator('.secret')
  await expect(secret).toBeVisible()
  const value = (await secret.textContent()) ?? ''
  expect(value.startsWith('avior_')).toBe(true)
  await expect(command).toContainText(value)
  await expect(command).not.toContainText('<YOUR_TOKEN>')

  // The other clients get the same token in their own shape.
  await page.getByRole('tab', { name: 'Codex' }).click()
  await expect(page.locator('.command').first()).toContainText('bearer_token_env_var')
  await page.getByRole('tab', { name: 'Cursor & others' }).click()
  await expect(page.locator('.command').first()).toContainText('"mcpServers"')
})

/*
 * FEAT-053. The value is shown once, and the list never carries it. The whole
 * design of the page rests on those two facts, so they are what is asserted:
 * the panel appears with the value, the row appears without it, and revoking
 * turns the row's state over.
 */
test('a token is shown once, listed without its value, and revoked', async ({ page }) => {
  await signIn(page)
  await page.getByRole('button', { name: 'Your account' }).click()
  await page.getByRole('link', { name: 'API tokens' }).click()
  await expect(page.getByRole('heading', { name: 'API tokens', exact: true })).toBeVisible()

  const name = `e2e-${Date.now()}`
  await page.getByPlaceholder('laptop agent').fill(name)
  await page.getByRole('button', { name: 'Create token' }).click()

  // The one place the value appears.
  const panel = page.locator('.minted')
  await expect(panel).toBeVisible()
  const value = (await panel.locator('code').textContent()) ?? ''
  expect(value.startsWith('avior_')).toBe(true)

  // The row is there, and does not carry the value.
  const row = page.locator('tbody tr', { hasText: name })
  await expect(row).toBeVisible()
  await expect(row).toContainText('live')
  await expect(row).not.toContainText(value)

  await panel.getByRole('button', { name: 'I have copied it' }).click()
  await expect(panel).toBeHidden()
  // And it is gone from the page, not merely hidden.
  await expect(page.getByText(value)).toHaveCount(0)

  await row.getByRole('button', { name: 'Revoke' }).click()
  await expect(row).toContainText('revoked')
  await expect(row.getByRole('button', { name: 'Revoke' })).toHaveCount(0)
})
