import { Binary, FileSearch, NotebookPen, ShieldCheck } from 'lucide-react'
import type { ComponentType } from 'react'

type Step = {
  icon: ComponentType<{ size?: number; strokeWidth?: number }>
  title: string
  body: string
  detail: string
}

const steps: Step[] = [
  {
    icon: Binary,
    title: 'Parse',
    body: 'A static parser reads the statement. Anything it cannot fully account for is denied.',
    detail: 'no default-allow branch',
  },
  {
    icon: FileSearch,
    title: 'Plan',
    body: 'Column permissions are checked against what the execution plan reads, not what the text names.',
    detail: 'EXPLAIN (FORMAT JSON), never ANALYZE',
  },
  {
    icon: NotebookPen,
    title: 'Audit',
    body: 'The audit row is written before execution, whatever the verdict. A crash still leaves a trace.',
    detail: 'written ahead of the run',
  },
  {
    icon: ShieldCheck,
    title: 'Run',
    body: 'Execution happens in a read-only transaction, so the engine enforces what the parser asserted.',
    detail: 'statement · lock · idle timeout',
  },
]

export function Guard() {
  return (
    <section id="guard" className="band">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow">The guard</p>
          <h2 className="section-title">Two checks that fail closed. Then a record. Then a read-only run.</h2>
          <p className="section-lede">
            An error is never an allow. When the guard cannot reach a decision, the statement is refused.
          </p>
        </div>
        <ol className="guard-grid">
          {steps.map(({ icon: Icon, title, body, detail }, i) => (
            <li key={title} className="tile">
              <div className="tile-top">
                <span className="tile-icon">
                  <Icon size={22} strokeWidth={1.75} />
                </span>
                <span className="tile-index">0{i + 1}</span>
              </div>
              <h3 className="tile-title">{title}</h3>
              <p className="tile-body">{body}</p>
              <code className="tile-detail">{detail}</code>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}
