import { ChartColumn, Columns3Cog, DatabaseZap, Gauge, KeyRound, UserCog } from 'lucide-react'
import type { ComponentType } from 'react'

type Feature = {
  icon: ComponentType<{ size?: number; strokeWidth?: number }>
  title: string
  body: string
}

const features: Feature[] = [
  {
    icon: Columns3Cog,
    title: 'Column restrictions',
    body: 'A restricted column is refused to everybody, including a DBA, until somebody unlocks it for a role and says why.',
  },
  {
    icon: DatabaseZap,
    title: 'Database access by role',
    body: 'Which roles reach which database — refused on every path, not merely hidden from a list.',
  },
  {
    icon: Gauge,
    title: 'Rate limits per caller',
    body: 'One statement budget per account, shared by every token and replica.',
  },
  {
    icon: KeyRound,
    title: 'Directory sign-in',
    body: 'LDAP for people, plus declared local accounts so a directory outage is not a lockout.',
  },
  {
    icon: UserCog,
    title: 'Service accounts',
    body: 'An account for an agent that is nobody’s person, granted access directly and audited as itself.',
  },
  {
    icon: ChartColumn,
    title: 'Audit sky',
    body: 'Seven days of submissions as hourly bars, allowed and refused apart, with a brush that narrows the log.',
  },
]

export function Features() {
  return (
    <section id="features" className="band band-2">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow">Policy</p>
          <h2 className="section-title">Every rule a production database needs.</h2>
          <p className="section-lede">
            Each one began as an acceptance spec, and each criterion has an integration test against a real
            PostgreSQL.
          </p>
        </div>
        <ul className="feature-grid">
          {features.map(({ icon: Icon, title, body }) => (
            <li key={title} className="feature">
              <span className="feature-icon">
                <Icon size={20} strokeWidth={1.75} />
              </span>
              <h3 className="feature-title">{title}</h3>
              <p className="feature-body">{body}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
