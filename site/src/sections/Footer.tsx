import { links } from '../brand'
import { Logo } from '../components/Logo'

export function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-row">
        <div className="footer-brand">
          <Logo height={22} />
          <p>Named for ε Carinae, the star sailors used to find south.</p>
        </div>
        <ul className="footer-links">
          <li>
            <a href={links.repo}>GitHub</a>
          </li>
          <li>
            <a href={links.features}>Feature specs</a>
          </li>
          <li>
            <a href={links.adr}>Decisions</a>
          </li>
          <li>
            <a href={links.license}>MIT License</a>
          </li>
        </ul>
      </div>
      <div className="container footer-credit">
        Built with <a href={links.astryx}>Astryx</a>. Starfield and decoding headline adapted from{' '}
        <a href={links.aimran}>AIMRAN Library</a>.
      </div>
    </footer>
  )
}
