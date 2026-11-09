import { Button } from '@astryxdesign/core/Button'

import { links } from '../brand'
import { Logo } from '../components/Logo'

const sections = [
  ['Guard', '#guard'],
  ['Writes', '#writes'],
  ['Agents', '#agents'],
  ['FAQ', '#faq'],
] as const

export function Nav() {
  return (
    <nav className="nav" aria-label="Primary">
      <div className="container nav-row">
        <div className="nav-start">
          <a href="#top" className="nav-home" aria-label="Avior home">
            <Logo height={26} />
          </a>
          <ul className="nav-links">
            {sections.map(([label, href]) => (
              <li key={href}>
                <a href={href}>{label}</a>
              </li>
            ))}
            <li>
              <a href={links.features}>Docs</a>
            </li>
          </ul>
        </div>
        <div className="nav-end">
          <a className="nav-github" href={links.repo}>
            GitHub
          </a>
          <Button variant="primary" size="sm" label="Get started" href="#start" />
        </div>
      </div>
    </nav>
  )
}
