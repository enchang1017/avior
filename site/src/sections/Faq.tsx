import { Collapsible } from '@astryxdesign/core/Collapsible'

import { questions } from '../content/faq'

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
