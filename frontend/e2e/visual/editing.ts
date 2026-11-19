import type { Page } from '@playwright/test'
import { answerJson } from '../redesign/fixtures'
import { administrator, serveFixedApi } from './api'

export const originalJSON = '{"id":9007199254740993,"tags":["paid",null],"enabled":true}'
const names = ['id', 'username', 'payload', 'note']

export async function serveEditableTable(page: Page, approval = false) {
  await serveFixedApi(page)
  await answerJson(page, '/auth/me', { ...administrator, writeApprovalRequired: approval })
  await answerJson(page, '/schema/columns', [
    { name: 'id', type: 'bigint', primaryKeyPosition: 1, nullable: false, denied: false },
    { name: 'username', type: 'text', nullable: false, denied: false },
    { name: 'payload', type: 'jsonb', nullable: false, denied: false },
    { name: 'note', type: 'text', nullable: true, denied: false },
  ])
  const records: (string | null)[][] = Array.from({ length: 51 }, (_, index) => [
    String(BigInt('9007199254740993') + BigInt(index)),
    index === 0 ? 'alice' : 'member-' + index,
    originalJSON,
    index === 0 ? '' : null,
  ])
  let offset = 0
  let limit = 51
  await page.route('**/api/v1/query/template', async (route) => {
    const body = route.request().postDataJSON() as { offset?: number; top?: number }
    offset = body.offset ?? 0
    limit = body.top ?? 51
    await route.fulfill({
      json: {
        statement:
          'SELECT id, username, payload, note FROM public.member LIMIT ' +
          limit +
          ' OFFSET ' +
          offset,
      },
    })
  })
  await page.route('**/api/v1/writes/row-current-template', async (route) => {
    const body = route.request().postDataJSON() as { original: { id: string } }
    await route.fulfill({ json: { statement: 'SELECT current:' + body.original.id } })
  })
  await page.route('**/api/v1/query/execute', (route) => {
    const { statement } = route.request().postDataJSON() as { statement: string }
    const rows = statement.startsWith('SELECT current:')
      ? records.filter((row) => row[0] === statement.slice('SELECT current:'.length))
      : records.slice(offset, offset + limit)
    return route.fulfill({
      json: {
        columns: names.map((name) => ({
          name,
          type: name === 'id' ? 'int8' : name === 'payload' ? 'jsonb' : 'text',
        })),
        rows,
        rowCount: rows.length,
        elapsedMs: 2,
        auditId: 'grid-audit',
        truncated: false,
        limits: { maxCost: 50, maxRows: 1000 },
      },
    })
  })
  await page.route('**/api/v1/writes/row-template', async (route) => {
    const body = route.request().postDataJSON() as {
      original: Record<string, string | null>
      changes: Record<string, string | null>
    }
    const quote = (value: string | null) =>
      value === null ? 'NULL' : "E'" + value.replaceAll('\\', '\\\\').replaceAll("'", "''") + "'"
    await route.fulfill({
      json: {
        statement:
          'UPDATE public.member\nSET ' +
          Object.entries(body.changes)
            .map(([name, value]) => name + ' = ' + quote(value))
            .join(', ') +
          '\nWHERE ' +
          Object.entries(body.original)
            .map(([name, value]) => name + (value === null ? ' IS NULL' : ' = ' + quote(value)))
            .join(' AND '),
      },
    })
  })
  await answerJson(page, '/writes/inspect', {
    decision: 'allow',
    reasons: [],
    estimatedRows: 1,
    kind: 'update',
    targetTable: 'public.member',
  })
  return records
}

export async function openEditableTable(page: Page): Promise<void> {
  await page.goto('/')
  await selectEditableTable(page)
}

export async function selectEditableTable(page: Page): Promise<void> {
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await page.getByRole('button', { name: 'Edit rows', exact: true }).click()
}
