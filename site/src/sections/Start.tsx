import { Button } from '@astryxdesign/core/Button'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'
import { Heading } from '@astryxdesign/core/Heading'
import { HStack } from '@astryxdesign/core/HStack'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

import { links } from '../brand'

const commands = `git clone https://github.com/enchang1017/avior.git
cd avior

make tools   # fetch the pinned linter
make dev     # the whole stack, seeded, with hot reload`

export function Start() {
  return (
    <section id="start" className="band">
      <div className="container split">
        <VStack gap={4} hAlign="start">
          <Text type="label" color="accent">
            Get started
          </Text>
          <Heading level={2} type="display-3" textWrap="balance">
            One command for the whole stack.
          </Heading>
          <Text type="large" color="secondary" as="p">
            You need Docker, Go 1.25, Node 22 and make. Everything else — linters, generators, the migration tool —
            runs from pinned versions that make fetches. The console comes up on localhost:5173 against a seeded
            PostgreSQL 18.
          </Text>
          <HStack gap={3} wrap="wrap">
            <Button variant="primary" label="Read the README" href={links.readme} />
            <Button variant="ghost" label="Browse the feature specs" href={links.features} />
          </HStack>
        </VStack>
        <div className="split-code">
          <CodeBlock title="Terminal" language="bash" code={commands} />
        </div>
      </div>
    </section>
  )
}
