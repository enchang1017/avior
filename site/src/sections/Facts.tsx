import { links } from '../brand'

const facts = [
  ['0', 'default-allow branches in the guard'],
  ['1', 'command decides done: make verify'],
  ['6', 'MCP tools, all read-only'],
  ['1', 'engine governed: PostgreSQL'],
] as const

export function Facts() {
  return (
    <section className="band band-2">
      <div className="container facts">
        <div className="facts-copy">
          <h2 className="section-title">
            No human reads this code. <span className="mark">Every rule is a gate.</span>
          </h2>
          <p className="section-lede">
            Every change is made by an agent, so every property that matters is machine-enforced. People read the{' '}
            <a href={links.features}>feature specs</a> and <a href={links.adr}>decision records</a> — the
            contract — never the implementation.
          </p>
        </div>
        <dl className="facts-grid">
          {facts.map(([n, label], i) => (
            <div key={i} className="fact">
              <dt className="fact-n">{n}</dt>
              <dd className="fact-label">{label}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}
