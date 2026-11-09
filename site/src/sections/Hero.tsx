import { Button } from '@astryxdesign/core/Button'
import { Check, X } from 'lucide-react'

import { links } from '../brand'
import { GuardConsole } from '../components/GuardConsole'
import { ScrambleText } from '../components/ScrambleText'

export function Hero() {
  return (
    <header className="hero">
      <div className="hero-glow" aria-hidden="true" />
      <div className="container hero-inner">
        <a className="hero-pill" href={links.postgresAdr}>
          <span className="hero-pill-tag">PostgreSQL</span>
          <span className="hero-pill-text">Avior governs PostgreSQL, and nothing else</span>
        </a>
        <h1 className="hero-title">
          <ScrambleText text="Every query to production," />
          <br />
          <span className="hero-title-mark">accounted for.</span>
        </h1>
        <p className="hero-lede">
          A governed SQL gateway. Operators and agents submit statements; Avior decides whether each one may
          run, records who asked, and returns the result read-only.
        </p>
        <div className="hero-actions">
          <Button variant="primary" size="lg" label="Run it locally" href="#start" />
          <Button variant="secondary" size="lg" label="Star on GitHub" href={links.repo} />
        </div>
      </div>

      <div className="hero-float hero-float-left" aria-hidden="true">
        <span className="float-chip float-chip-allow">
          <Check size={14} strokeWidth={2.5} /> Allowed
        </span>
        <p className="float-title">Regional player lookup</p>
        <p className="float-body">Read-only · 50 rows · audited as #48213</p>
        <div className="float-meter">
          <span className="float-meter-label">Plan cost</span>
          <span className="float-meter-value">12 / 500</span>
        </div>
        <div className="float-bar">
          <span style={{ width: '18%' }} />
        </div>
      </div>

      <div className="hero-float hero-float-right" aria-hidden="true">
        <span className="float-chip float-chip-deny">
          <X size={14} strokeWidth={2.5} /> Refused
        </span>
        <p className="float-title">players.email</p>
        <p className="float-body">A restricted column, read through a view the statement never named.</p>
        <span className="float-ask">Ask an approver to unlock it</span>
      </div>

      <div className="container hero-console">
        <GuardConsole />
      </div>
    </header>
  )
}
