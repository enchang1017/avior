import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import SchemaTree from './SchemaTree.vue'
import type { ColumnRef, TableRef } from '@/shared/api'

const memberTable: TableRef = { schema: 'public', name: 'member', kind: 'table' }
const memberView: TableRef = { schema: 'public', name: 'member_view', kind: 'view' }
const tables: TableRef[] = [memberTable, memberView]

const columns: ColumnRef[] = [
  { name: 'member_id', type: 'integer', nullable: false, denied: false },
  { name: 'login_name', type: 'text', nullable: true, denied: false },
  { name: 'id_card_number', type: 'text', nullable: false, denied: true },
]

function render(overrides: Partial<InstanceType<typeof SchemaTree>['$props']> = {}) {
  return mount(SchemaTree, {
    props: {
      databases: ['aviorfixture'],
      tables,
      columns,
      database: 'aviorfixture',
      table: memberTable,
      selected: new Set(['member_id', 'login_name']),
      allSelected: true,
      orderBy: '',
      descending: true,
      orderableColumns: ['member_id', 'login_name'],
      temporalColumns: [],
      busy: false,
      ...overrides,
    },
  })
}

describe('SchemaTree', () => {
  it('lists every column with its type, denied ones included', () => {
    const wrapper = render()
    const text = wrapper.text()

    for (const want of ['member_id', 'integer', 'login_name', 'text', 'id_card_number', 'denied']) {
      expect(text).toContain(want)
    }
    expect(wrapper.findAll('.column-list li')).toHaveLength(3)
  })

  // Offering the checkbox would let an operator build a statement certain to be
  // refused, and the refusal would arrive after they pressed Insert.
  it('disables the checkbox of a denied column and no other', () => {
    const wrapper = render()
    const disabled = wrapper
      .findAll<HTMLInputElement>('.column-list input[type="checkbox"]')
      .map((box) => box.element.disabled)

    expect(disabled).toEqual([false, false, true])
  })

  // A view and a table read the same in a list of names, and reading a view is
  // not the same thing at all: the plan resolves it to the base tables.
  it('distinguishes a view from a table by its icon', () => {
    const wrapper = render()
    const nodes = wrapper.findAll('.tables .node')

    expect(nodes.map((node) => node.text())).toEqual(['memberpublic', 'member_viewpublic'])
    expect(nodes[0]?.find('svg[data-icon="table"]').exists()).toBe(true)
    expect(nodes[1]?.find('svg[data-icon="eye"]').exists()).toBe(true)
  })

  // FEAT-075. The parent is listed once and says how many partitions sit
  // behind it; the partitions themselves are not in the list it was given.
  it('marks a partitioned table with its partition count', () => {
    const rounds: TableRef = {
      schema: 'public',
      name: 'rounds',
      kind: 'partitioned',
      partitions: 28,
    }
    const wrapper = render({ tables: [memberTable, rounds] })
    const node = wrapper.findAll('.tables .node')[1]

    expect(node?.find('.partitioned').text()).toBe('partitioned · 28')
    expect(node?.find('svg[data-icon="layers"]').exists()).toBe(true)
    expect(wrapper.findAll('.tables .node')[0]?.find('.partitioned').exists()).toBe(false)
  })

  it('reports how many columns are selected', () => {
    expect(render().text()).toContain('2 of 3 columns')
  })

  it('cannot insert with nothing selected', () => {
    const wrapper = render({ selected: new Set(), allSelected: false })
    const insert = wrapper.find<HTMLButtonElement>('button.insert')

    expect(insert.element.disabled).toBe(true)
  })

  it('emits insert when the button is pressed', async () => {
    const wrapper = render()

    await wrapper.find('button.insert').trigger('click')

    expect(wrapper.emitted('insert')).toHaveLength(1)
  })

  it('emits the chosen table rather than its label', async () => {
    const wrapper = render()

    await wrapper.findAll('.tables .node')[1]?.trigger('click')

    expect(wrapper.emitted('choose-table')).toEqual([[memberView]])
  })

  // Clicking the open database collapses it, which is how a tree behaves.
  it('emits null when the open database is clicked again', async () => {
    const wrapper = render()

    await wrapper.find('.node.database').trigger('click')

    expect(wrapper.emitted('choose-database')).toEqual([[null]])
  })

  it('filters the tree by name', async () => {
    const wrapper = render()

    await wrapper.find('input[type="search"]').setValue('view')

    const names = wrapper.findAll('.tables .node-label').map((node) => node.text())
    expect(names).toEqual(['member_view'])
  })

  it('says so when nothing matches', async () => {
    const wrapper = render()

    await wrapper.find('input[type="search"]').setValue('zzz')

    expect(wrapper.text()).toContain('Nothing matches')
  })

  // FEAT-074 AC-6: an empty list is only an answer once the request came back.
  it('shows it is loading the databases, never that nothing matches, while they are pending', () => {
    const wrapper = render({ databases: [], tables: [], columns: [], database: null, busy: true })

    expect(wrapper.find('[role="status"]').text()).toContain('Loading databases')
    expect(wrapper.text()).not.toContain('Nothing matches')
  })

  it('shows it is loading the tables, never that none match, while they are pending', () => {
    const wrapper = render({ tables: [], columns: [], table: null, busy: true })

    expect(wrapper.find('.tables [role="status"]').text()).toContain('Loading tables')
    expect(wrapper.text()).not.toContain('No tables match')
  })

  it('says no tables match once an answer said so', () => {
    const wrapper = render({ tables: [], columns: [], table: null, busy: false })

    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('No tables match')
  })

  it('shows no column list until a table is chosen', () => {
    const wrapper = render({ tables: [], columns: [], table: null })

    expect(wrapper.find('.column-list').exists()).toBe(false)
    expect(wrapper.findAll('.tables .node')).toHaveLength(0)
  })

  describe('ordering', () => {
    // What an operator almost always means by "order by" is the newest rows, so
    // the dates are marked and sit at the top rather than being hunted for.
    it('marks which of the columns are dates', async () => {
      const tree = render({
        orderableColumns: ['register_time', 'member_id'],
        temporalColumns: ['register_time'],
      })
      await tree.find('#order-column').trigger('focus')

      const options = tree.findAll('.option').map((option) => option.text())
      expect(options).toEqual(['nothing', 'register_time · date', 'member_id'])
    })

    it('asks its parent to order, by column name and not by label', async () => {
      const tree = render({
        orderableColumns: ['register_time'],
        temporalColumns: ['register_time'],
      })
      await tree.find('#order-column').trigger('focus')
      await tree.findAll('.option')[1]?.trigger('mousedown')

      expect(tree.emitted('order-by')?.at(-1)).toEqual(['register_time'])
    })

    // Ordering by nothing is a choice, and the ordinary one: a statement with
    // no ORDER BY is one PostgreSQL may answer in any order, and saying so is
    // more honest than pretending the rows arrive sorted.
    it('offers a way back to no ordering', async () => {
      const tree = render({ orderBy: 'register_time', orderableColumns: ['register_time'] })
      await tree.find('#order-column').trigger('focus')
      await tree.findAll('.option')[0]?.trigger('mousedown')

      expect(tree.emitted('order-by')?.at(-1)).toEqual([''])
    })

    it('asks its parent to turn the direction round', async () => {
      const tree = render({ orderBy: 'register_time', orderableColumns: ['register_time'] })

      await tree.find('.direction').trigger('click')

      expect(tree.emitted('toggle-direction')).toHaveLength(1)
    })

    // Turning round an ordering that is not there does nothing anybody can see,
    // so the control says so rather than looking live.
    it('cannot turn a direction round when nothing is ordered', () => {
      const tree = render({ orderBy: '' })

      expect(tree.find('.direction').attributes('disabled')).toBeDefined()
    })
  })
})
