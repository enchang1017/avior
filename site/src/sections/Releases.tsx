import { Button } from '@astryxdesign/core/Button'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'

import { links } from '../brand'

const lanes = [
  {
    name: 'Staging',
    how: 'Declarative sync',
    body: 'Every merge applies the schema files directly. The end state is right, and nothing is recorded.',
  },
  {
    name: 'Production',
    how: 'Versioned migrations',
    body: 'Generated once, reviewed as SQL, applied in order through Avior, and recorded file by file.',
  },
] as const

const project = [
  ['Generate', 'Atlas diffs the table files against the replayed history. Functions and grants are copied as they are.'],
  ['Lint', 'squawk and the kit’s own rules refuse what would lock or lose data.'],
  ['Verify', 'Both lanes built from nothing must be the same database, privileges included.'],
  ['Submit', 'CI sends what the target has not run, with the digest it should have now.'],
] as const

const governed = [
  ['Drift check', 'The target’s live schema against that digest. A hand edit refuses the release.'],
  ['Approve', 'A release approver reads the SQL that will run.'],
  ['Apply', 'In order, as the database’s schema owner, one transaction per file.'],
  ['Record', 'Who, when, how long, and the outcome of every file.'],
] as const

const commands = `cd my-database-project
avior/tools/release-kit/generate-migration.sh add_sort_order
avior/tools/release-kit/lint-migrations.sh
avior/tools/release-kit/verify-migrations.sh`

export function Releases() {
  return (
    <section id="releases" className="band band-2">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow">Schema releases</p>
          <h2 className="section-title">A schema change ships as a reviewed migration, never as a diff against production.</h2>
          <p className="section-lede">
            The database project generates, lints and verifies. Avior proves production has not moved, waits for an
            approver, applies and records.
          </p>
        </div>

        <ul className="lanes">
          {lanes.map((lane) => (
            <li key={lane.name} className="lane">
              <p className="lane-name">{lane.name}</p>
              <h3 className="lane-how">{lane.how}</h3>
              <p className="lane-body">{lane.body}</p>
            </li>
          ))}
        </ul>

        <div className="pipeline">
          <div className="pipeline-side">
            <p className="pipeline-label">In the database project</p>
            <ol className="pipeline-steps">
              {project.map(([title, body], i) => (
                <li key={title} className="pipeline-step">
                  <span className="pipeline-n">{i + 1}</span>
                  <div>
                    <h3 className="pipeline-title">{title}</h3>
                    <p className="pipeline-body">{body}</p>
                  </div>
                </li>
              ))}
            </ol>
          </div>
          <div className="pipeline-side pipeline-side-avior">
            <p className="pipeline-label">In Avior</p>
            <ol className="pipeline-steps" start={project.length + 1}>
              {governed.map(([title, body], i) => (
                <li key={title} className="pipeline-step">
                  <span className="pipeline-n">{project.length + i + 1}</span>
                  <div>
                    <h3 className="pipeline-title">{title}</h3>
                    <p className="pipeline-body">{body}</p>
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </div>

        <div className="releases-foot">
          <CodeBlock title="Every day, in the database project" language="bash" code={commands} />
          <div className="releases-links">
            <p className="releases-note">
              The release kit runs from an Avior checkout, so the scripts and the schema digest Avior checks always
              come from the same commit.
            </p>
            <div className="releases-actions">
              <Button variant="primary" size="lg" label="Release runbook" href={links.releaseRunbook} />
              <Button variant="secondary" size="lg" label="The release kit" href={links.releaseKit} />
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
