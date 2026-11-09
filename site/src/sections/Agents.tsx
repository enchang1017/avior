import { CodeBlock } from '@astryxdesign/core/CodeBlock'

const tools = [
  ['whoami', 'who the token speaks for'],
  ['list_databases', 'the databases this role may reach'],
  ['list_tables', 'tables and views in one database'],
  ['describe_table', 'columns, with restricted ones marked'],
  ['sample_rows', 'a few rows, through the guard'],
  ['run_sql', 'one statement, guarded and audited'],
] as const

const setup = `claude mcp add --transport http avior \\
  https://avior.example.com/api/v1/mcp \\
  --header "Authorization: Bearer <token>"`

export function Agents() {
  return (
    <section id="agents" className="band">
      <div className="container agents">
        <div className="agents-copy">
          <p className="eyebrow">Built for agents</p>
          <h2 className="section-title">
            Your agent gets a <span className="mark">token</span>, not a connection string.
          </h2>
          <p className="section-lede">
            Avior speaks the Model Context Protocol itself. Every tool call goes through the same guard, access
            table and audit as the console, and answers with structured output: rows as arrays, the audit id,
            numbers with every digit.
          </p>
          <CodeBlock title="Connect Claude Code" language="bash" code={setup} />
        </div>
        <ul className="tools">
          {tools.map(([name, what]) => (
            <li key={name} className="tool">
              <code className="tool-name">{name}</code>
              <span className="tool-what">{what}</span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
