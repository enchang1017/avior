import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VConnectionBar from '../VConnectionBar.vue'
import type { CurrentUser } from '@/shared/api'

function render(
  database: string | null,
  table: string | null = null,
  environment: CurrentUser['environment'] = 'production',
) {
  return mount(VConnectionBar, {
    props: { instance: 'local-postgres', environment, database, table },
  })
}

describe('VConnectionBar', () => {
  // The banner is the only place the target is stated. An operator who has to
  // scroll back to a tree to find out which database they are about to read has
  // already been given the chance to get it wrong.
  it('names the whole target once a table is chosen', () => {
    const crumbs = render('aviorfixture', 'public.member')
      .findAll('.crumb')
      .map((crumb) => crumb.text())

    expect(crumbs).toEqual(['local-postgres', 'aviorfixture', 'public.member'])
  })

  it('names as much of the target as is known', () => {
    expect(
      render(null)
        .findAll('.crumb')
        .map((c) => c.text()),
    ).toEqual(['local-postgres'])
    expect(
      render('aviorfixture')
        .findAll('.crumb')
        .map((c) => c.text()),
    ).toEqual(['local-postgres', 'aviorfixture'])
  })

  // Amber once a statement could actually run somewhere that cannot be undone,
  // quiet before that. A warning that is always on is a warning nobody reads.
  it('arms itself only on production, and only once a database is chosen', () => {
    expect(render(null, null, 'production').find('.risk').classes()).toContain('idle')
    expect(render('aviorfixture', null, 'production').find('.risk').classes()).toContain('armed')
  })

  // The bug this replaced: the word "production" was written into the template,
  // so every deployment claimed to be reading it. A staging banner that says
  // production is the warning teaching operators to read past the warning.
  it('names the deployment it is actually reading, and stays quiet off production', () => {
    for (const environment of ['development', 'test', 'staging'] as const) {
      const bar = render('aviorfixture', null, environment)

      expect(bar.find('.env-name').text()).toBe(environment)
      expect(bar.text()).not.toContain('production')
      expect(bar.find('.risk').classes()).toContain('ready')
      expect(bar.find('.risk').classes()).not.toContain('armed')
    }

    const production = render('aviorfixture', null, 'production')
    expect(production.find('.env-name').text()).toBe('production')
    expect(production.find('.env-name').classes()).toContain('text-env-production')
  })

  it('tells the operator what to do first when nothing is chosen', () => {
    expect(render(null).text()).toContain('Pick a database')
  })

  // FEAT-065 AC-6: the reachable databases and nothing else, and a pick is reported.
  it('offers the databases it is given and reports the one chosen', async () => {
    const bar = mount(VConnectionBar, {
      props: {
        instance: 'local',
        environment: 'staging',
        database: 'ledger',
        table: null,
        databases: ['aviorfixture', 'ledger'],
      },
      attachTo: document.body,
    })
    await bar.find('button.trigger').trigger('click')
    const options = bar.findAll('[role="menuitemradio"]')
    expect(options.map((option) => option.text())).toEqual(['aviorfixture', 'ledger'])
    expect(options[1]?.attributes('aria-checked')).toBe('true')
    await options[0]?.trigger('click')
    expect(bar.emitted('choose')).toEqual([['aviorfixture']])
    bar.unmount()
  })
})
