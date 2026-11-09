import { Code } from '@astryxdesign/core/Code'
import { Heading } from '@astryxdesign/core/Heading'
import { Link } from '@astryxdesign/core/Link'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

import { links } from '../brand'

const invariants = [
  ['The guard fails closed', 'Any statement it cannot fully account for is denied.'],
  ['A verdict is never ambiguous', 'Allow carries no reasons; deny carries at least one the operator can act on.'],
  ['An error is never an allow', 'An unreachable server or an unparseable plan refuses the statement.'],
  ['The plan is the authority', 'Column permissions are checked against what the plan reads, not the text.'],
  ['Audited before it runs', 'The audit row lands ahead of execution, whatever the verdict.'],
] as const

export function Harness() {
  return (
    <section id="harness" className="band band-raised">
      <div className="container split">
        <VStack gap={4}>
          <Text type="label" color="accent">
            How it is built
          </Text>
          <Heading level={2} type="display-3" textWrap="balance">
            No human reads this code. Every property that matters is a gate.
          </Heading>
          <Text type="large" color="secondary" as="p">
            Every change is made by an agent, and review by reading is not part of the process. So every quality
            property is machine-enforced, and <Code>make verify</Code> exiting zero is the definition of done.
            People read the <Link href={links.features}>feature specs</Link> and the{' '}
            <Link href={links.adr}>decision records</Link>, never the implementation.
          </Text>
        </VStack>
        <ul className="invariants">
          {invariants.map(([title, body]) => (
            <li key={title}>
              <Text weight="semibold" display="block">
                {title}
              </Text>
              <Text color="secondary" display="block">
                {body}
              </Text>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

