import { Bot, Database, GitBranch, KeyRound, LayoutDashboard, Server, ShieldCheck, Users } from 'lucide-react'
import type { ComponentType } from 'react'

type Icon = ComponentType<{ size?: number; strokeWidth?: number }>

type Node = {
  icon: Icon
  title: string
  body: string
  detail?: string
}

const callers: Node[] = [
  { icon: Users, title: 'Operators', body: 'A browser session, signed in through the directory.' },
  { icon: Bot, title: 'Agents', body: 'An API token over HTTP or the MCP endpoint. It only reads.' },
  { icon: GitBranch, title: 'Database projects', body: 'CI with a release token. It submits migrations, never applies them.' },
]

const avior: Node[] = [
  {
    icon: LayoutDashboard,
    title: 'Console',
    body: 'The Vue app, built once and served as files. It holds no API address.',
    detail: 'nginx · static',
  },
  {
    icon: ShieldCheck,
    title: 'Gateway',
    body: 'The guard, the audit, approvals and releases. Stateless, so replicas scale out.',
    detail: 'Go · 2–4 replicas',
  },
  {
    icon: KeyRound,
    title: 'Directory',
    body: 'LDAP vouches for people. Declared local accounts cover an outage.',
    detail: 'sign-in only',
  },
]

const credentials = [
  ['avior_governed', 'every operator read'],
  ['avior_writer', 'approved writes'],
  ['avior_deployer', 'applying releases'],
  ['avior_provisioner', 'creating databases and roles'],
] as const

function NodeCard({ icon: Icon, title, body, detail }: Node) {
  return (
    <li className="arch-node">
      <span className="arch-node-icon">
        <Icon size={18} strokeWidth={1.75} />
      </span>
      <div className="arch-node-text">
        <h3 className="arch-node-title">{title}</h3>
        <p className="arch-node-body">{body}</p>
        {detail ? <code className="arch-node-detail">{detail}</code> : null}
      </div>
    </li>
  )
}

export function Architecture() {
  return (
    <section id="architecture" className="band">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow">Architecture</p>
          <h2 className="section-title">One gateway between every caller and every database.</h2>
          <p className="section-lede">
            Nothing reaches a governed database except through Avior, and Avior reaches it with a different
            credential for each kind of act.
          </p>
        </div>

        <div className="arch">
          <div className="arch-tier">
            <p className="arch-tier-label">Who reaches it</p>
            <ul className="arch-row">
              {callers.map((node) => (
                <NodeCard key={node.title} {...node} />
              ))}
            </ul>
          </div>

          <p className="arch-link">
            <span className="arch-link-line" aria-hidden="true" />
            <span className="arch-link-text">HTTPS · one ingress · /api to the gateway, the rest to the console</span>
          </p>

          <div className="arch-tier arch-tier-core">
            <p className="arch-tier-label">
              <Server size={14} strokeWidth={1.75} /> Avior
            </p>
            <ul className="arch-row">
              {avior.map((node) => (
                <NodeCard key={node.title} {...node} />
              ))}
            </ul>
          </div>

          <p className="arch-link">
            <span className="arch-link-line" aria-hidden="true" />
            <span className="arch-link-text">one PostgreSQL role per act, never a shared superuser</span>
          </p>

          <div className="arch-tier">
            <p className="arch-tier-label">
              <Database size={14} strokeWidth={1.75} /> One PostgreSQL server per environment
            </p>
            <div className="arch-db">
              <div className="arch-db-store">
                <h3 className="arch-node-title">aviorDB</h3>
                <p className="arch-node-body">
                  Avior’s own store: accounts, the audit, write requests, the release ledger. Never governed.
                </p>
              </div>
              <div className="arch-db-governed">
                <h3 className="arch-node-title">Governed databases</h3>
                <p className="arch-node-body">One per service, each reached only through these roles:</p>
                <ul className="arch-roles">
                  {credentials.map(([role, act]) => (
                    <li key={role} className="arch-role">
                      <code className="arch-role-name">{role}</code>
                      <span className="arch-role-act">{act}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
