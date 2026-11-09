import { Code } from '@astryxdesign/core/Code'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'
import { Heading } from '@astryxdesign/core/Heading'
import { HStack } from '@astryxdesign/core/HStack'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

const tools = ['whoami', 'list_databases', 'list_tables', 'describe_table', 'sample_rows', 'run_sql'] as const

const setup = `claude mcp add --transport http avior \\
  https://avior.example.com/api/v1/mcp \\
  --header "Authorization: Bearer <token>"`

export function Agents() {
  return (
    <section id="agents" className="band">
      <div className="container split">
        <VStack gap={4}>
          <Text type="label" color="accent">
            Built for agents
          </Text>
          <Heading level={2} type="display-3" textWrap="balance">
            Agents read through the same gate as people.
          </Heading>
          <Text type="large" color="secondary" as="p">
            Avior speaks the Model Context Protocol itself. An agent connects with a token an operator minted,
            or as a service account that is nobody&apos;s person, and every call goes through the same guard,
            access table and audit as the console. No server on the laptop.
          </Text>
          <HStack gap={2} wrap="wrap">
            {tools.map((t) => (
              <Code key={t}>{t}</Code>
            ))}
          </HStack>
          <Text type="supporting" as="p">
            Every tool declares an output schema: rows as arrays, the audit id, numbers with every digit — and a
            refusal carries none.
          </Text>
        </VStack>
        <div className="split-code">
          <CodeBlock title="Connect Claude Code" language="bash" code={setup} />
        </div>
      </div>
    </section>
  )
}
