const refusals = [
  'only SELECT and WITH may lead',
  'restricted column read through a view',
  'routine outside pg_catalog',
  'plan cost above the cap',
  'database not granted to this role',
  'statement budget spent for this account',
  'the plan would not parse',
  'target server unreachable',
] as const

export function Refusals() {
  return (
    <section className="refusals" aria-labelledby="refusals-title">
      <p id="refusals-title" className="eyebrow">
        Refused before it reaches production
      </p>
      <div className="marquee">
        <ul className="marquee-track">
          {[...refusals, ...refusals].map((r, i) => (
            <li key={i} aria-hidden={i >= refusals.length}>
              <span className="marquee-x">×</span>
              {r}
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
