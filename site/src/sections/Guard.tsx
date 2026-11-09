import { Badge } from '@astryxdesign/core/Badge'
import { Code } from '@astryxdesign/core/Code'
import { Heading } from '@astryxdesign/core/Heading'
import { Table } from '@astryxdesign/core/Table'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

const steps = [
  {
    n: '01',
    title: 'Parse',
    body: 'A static parser reads the statement. Only SELECT and WITH may lead. Anything the parser cannot fully account for is denied — there is no default-allow branch anywhere in it.',
  },
  {
    n: '02',
    title: 'Plan',
    body: 'The statement is planned with EXPLAIN (FORMAT JSON), never ANALYZE. Column permissions are checked against the objects the plan resolves, so a view cannot smuggle a column the text never names.',
  },
  {
    n: '03',
    title: 'Record',
    body: 'The audit row is written before the statement runs, whatever the verdict. A statement that times out or crashes the process still left a trace.',
  },
  {
    n: '04',
    title: 'Run',
    body: 'Execution happens in a read-only transaction with a statement, lock and idle timeout set, so the engine enforces what the parser asserted.',
  },
] as const

interface Example extends Record<string, unknown> {
  id: string
  sql: string
  allowed: boolean
  why: string
}

const examples: Example[] = [
  { id: 'select', sql: 'SELECT id, name FROM players LIMIT 50', allowed: true, why: 'Read-only, every column visible to the role' },
  { id: 'delete', sql: 'DELETE FROM players', allowed: false, why: 'Only SELECT and WITH may lead' },
  { id: 'view', sql: 'SELECT * FROM player_contact_view', allowed: false, why: 'The plan reads a restricted column through the view' },
  { id: 'routine', sql: 'SELECT refresh_balances()', allowed: false, why: 'A routine outside pg_catalog is refused, not guessed at' },
]

export function Guard() {
  return (
    <section id="guard" className="band">
      <div className="container">
        <VStack gap={8}>
          <VStack gap={3} maxWidth={720}>
            <Text type="label" color="accent">
              How a statement reaches production
            </Text>
            <Heading level={2} type="display-3" textWrap="balance">
              Two independent checks, then a record, then a read-only run.
            </Heading>
            <Text type="large" color="secondary" as="p">
              Both checks fail closed. An error is never an allow: when the guard cannot reach a decision, the
              statement is refused.
            </Text>
          </VStack>

          <ol className="steps">
            {steps.map((step) => (
              <li key={step.n} className="step">
                <span className="step-n">{step.n}</span>
                <Heading level={3}>{step.title}</Heading>
                <Text color="secondary" as="p">
                  {step.body}
                </Text>
              </li>
            ))}
          </ol>

          <VStack gap={2}>
            <div className="table-frame">
              <Table<Example>
                data={examples}
                idKey="id"
                dividers="rows"
                columns={[
                  { key: 'sql', header: 'Statement', renderCell: (row) => <Code>{row.sql}</Code> },
                  {
                    key: 'allowed',
                    header: 'Verdict',
                    width: { type: 'pixel', value: 120 },
                    renderCell: (row) =>
                      row.allowed ? <Text color="secondary">allow</Text> : <Badge variant="error" label="deny" />,
                  },
                  { key: 'why', header: 'Reason the operator sees' },
                ]}
              />
            </div>
            <Text type="supporting">Illustrative statements against a hypothetical schema.</Text>
          </VStack>
        </VStack>
      </div>
    </section>
  )
}
