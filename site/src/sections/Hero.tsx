import { Button } from '@astryxdesign/core/Button'

import { links } from '../brand'
import { GuardConsole } from '../components/GuardConsole'
import { ScrambleText } from '../components/ScrambleText'
import { Starfield } from '../components/Starfield'

export function Hero() {
  return (
    <header className="hero">
      <Starfield />
      <div className="hero-veil" />
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
        <div className="hero-console">
          <GuardConsole />
        </div>
      </div>
    </header>
  )
}
