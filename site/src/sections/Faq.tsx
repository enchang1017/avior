import { Collapsible } from '@astryxdesign/core/Collapsible'

const questions = [
  [
    'Does it work with MySQL or SQL Server?',
    'No. Avior governs PostgreSQL, and nothing else — not behind an adapter, not kept for later. One PostgreSQL server per environment, one database per service.',
  ],
  [
    'Does planning a statement run it?',
    'No. Avior plans with EXPLAIN (FORMAT JSON) and never ANALYZE, so the plan costs nothing on the target and the cost and row estimates it returns are what the limits apply to.',
  ],
  [
    'Can operators change data at all?',
    'Yes, through a governed write request. A developer asks, an approver reads what it would touch and how many rows, and Avior runs it — keeping what it overwrote in the same transaction.',
  ],
  [
    'What happens when the guard is unsure?',
    'It refuses. An unreachable server, a plan that will not parse, or syntax the parser does not know all end in a denial with a reason the operator can act on.',
  ],
  [
    'Is it free?',
    'Yes. Avior is MIT licensed. The frontend is Vue 3, the backend Go, and the whole stack runs locally with one command.',
  ],
] as const

export function Faq() {
  return (
    <section id="faq" className="band">
      <div className="container faq">
        <div className="faq-head">
          <h2 className="section-title">Questions</h2>
          <p className="section-lede">The answers live in the decision records too.</p>
        </div>
        <div className="faq-list">
          {questions.map(([q, a], i) => (
            <div key={q} className="faq-item">
              <Collapsible
                defaultIsOpen={i === 0}
                trigger={
                  <span className="faq-q">
                    <span className="faq-n">0{i + 1}</span>
                    {q}
                  </span>
                }
              >
                <p className="faq-a">{a}</p>
              </Collapsible>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
