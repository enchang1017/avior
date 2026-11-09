import { Button } from '@astryxdesign/core/Button'
import { HStack } from '@astryxdesign/core/HStack'
import { Link } from '@astryxdesign/core/Link'

import { links } from '../brand'
import { Logo } from '../components/Logo'

const sections = [
  ['How it works', '#guard'],
  ['Features', '#features'],
  ['Agents', '#agents'],
  ['Get started', '#start'],
] as const

export function Nav() {
  return (
    <nav className="nav" aria-label="Primary">
      <div className="container nav-row">
        <a href="#top" className="nav-home" aria-label="Avior home">
          <Logo height={28} />
        </a>
        <HStack gap={5} vAlign="center">
          <div className="nav-links">
            {sections.map(([label, href]) => (
              <Link key={href} href={href} color="secondary">
                {label}
              </Link>
            ))}
          </div>
          <Button variant="secondary" size="sm" label="GitHub" href={links.repo} />
        </HStack>
      </div>
    </nav>
  )
}
