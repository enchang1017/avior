import { Badge } from '@astryxdesign/core/Badge'
import { Button } from '@astryxdesign/core/Button'
import { Heading } from '@astryxdesign/core/Heading'
import { HStack } from '@astryxdesign/core/HStack'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

import { links } from '../brand'
import { ScrambleText } from '../components/ScrambleText'
import { Starfield } from '../components/Starfield'

export function Hero() {
  return (
    <header className="hero">
      <Starfield />
      <div className="hero-veil" />
      <div className="container hero-content">
        <VStack gap={5} hAlign="start">
          <HStack gap={2} wrap="wrap">
            <Badge variant="blue" label="Open source" />
            <Badge variant="orange" label="PostgreSQL" />
          </HStack>
          <Heading level={1} type="display-1" textWrap="balance">
            <ScrambleText text="Every query to production, accounted for." />
          </Heading>
          <Text type="large" color="secondary" as="p" textWrap="pretty">
            Avior is a governed SQL gateway. Operators submit read-only statements against production
            databases; Avior decides whether each one may run, records who asked for it, and returns the
            result.
          </Text>
          <HStack gap={3} wrap="wrap">
            <Button variant="primary" size="lg" label="Get started" href="#start" />
            <Button variant="secondary" size="lg" label="View on GitHub" href={links.repo} />
          </HStack>
        </VStack>
      </div>
    </header>
  )
}
