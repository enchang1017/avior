import { Button } from '@astryxdesign/core/Button'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'

import { links } from '../brand'

const commands = `git clone https://github.com/enchang1017/avior.git
cd avior && make dev`

export function Start() {
  return (
    <section id="start" className="band">
      <div className="container">
        <div className="cta">
          <div className="cta-copy">
            <h2 className="cta-title">
              The whole stack, <span className="cta-mark">seeded</span>, in one command.
            </h2>
            <p className="cta-lede">
              Docker, Go 1.25, Node 22 and make. Everything else runs from pinned versions. The console comes up on
              localhost:5173 against PostgreSQL 18.
            </p>
            <div className="cta-actions">
              <Button variant="secondary" size="lg" label="Read the README" href={links.readme} />
            </div>
          </div>
          <div className="cta-code">
            <CodeBlock language="bash" code={commands} hasLanguageLabel={false} isWrapped />
          </div>
        </div>
      </div>
    </section>
  )
}
