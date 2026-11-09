import { IconButton } from '@astryxdesign/core/IconButton'

import { links } from '../brand'
import type { ColorMode } from '../colorMode'
import { GitHubMark } from '../components/GitHubMark'
import { Logo } from '../components/Logo'
import { ModeSwitch } from '../components/ModeSwitch'
import { SiteSearch } from '../components/SiteSearch'

const sections = [
  ['Guard', '#guard'],
  ['Writes', '#writes'],
  ['Agents', '#agents'],
  ['FAQ', '#faq'],
] as const

type NavProps = {
  mode: ColorMode
  onModeChange: (mode: ColorMode) => void
}

export function Nav({ mode, onModeChange }: NavProps) {
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
          <IconButton variant="ghost" label="Avior on GitHub" tooltip="GitHub" icon={<GitHubMark />} href={links.repo} />
          <SiteSearch />
          <div className="nav-mode">
            <ModeSwitch mode={mode} onChange={onModeChange} />
          </div>
        </div>
      </div>
    </nav>
  )
}
