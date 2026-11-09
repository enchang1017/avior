import { HStack } from '@astryxdesign/core/HStack'
import { Link } from '@astryxdesign/core/Link'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

import { links } from '../brand'
import { Logo } from '../components/Logo'

export function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-row">
        <VStack gap={2} hAlign="start">
          <Logo height={24} />
          <Text type="supporting">Named for ε Carinae, the star sailors used to find south.</Text>
        </VStack>
        <VStack gap={2} hAlign="end">
          <HStack gap={4} wrap="wrap">
            <Link href={links.repo} color="secondary">
              GitHub
            </Link>
            <Link href={links.features} color="secondary">
              Feature specs
            </Link>
            <Link href={links.license} color="secondary">
              MIT License
            </Link>
          </HStack>
          <Text type="supporting">
            Built with <Link href={links.astryx} color="secondary">Astryx</Link>. Starfield and decoding headline
            adapted from <Link href={links.aimran} color="secondary">AIMRAN Library</Link>.
          </Text>
        </VStack>
      </div>
    </footer>
  )
}
