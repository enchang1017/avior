import { Button } from '@astryxdesign/core/Button'
import { CodeBlock } from '@astryxdesign/core/CodeBlock'
import { Container, DatabaseBackup, LayoutDashboard } from 'lucide-react'
import type { ComponentType } from 'react'

import { links } from '../brand'

type Image = {
  icon: ComponentType<{ size?: number; strokeWidth?: number }>
  title: string
  body: string
  detail: string
}

const images: Image[] = [
  {
    icon: Container,
    title: 'Gateway',
    body: 'One static binary on distroless, nonroot, no shell. It reports its commit on every start.',
    detail: 'backend/Dockerfile · :8080',
  },
  {
    icon: LayoutDashboard,
    title: 'Console',
    body: 'The built app behind nginx. The same image is right in every environment.',
    detail: 'frontend/Dockerfile · :8080',
  },
  {
    icon: DatabaseBackup,
    title: 'Migrate',
    body: 'goose and Avior’s own migrations, run as a job by the owner of aviorDB — never by the gateway.',
    detail: 'deploy/migrate/Dockerfile',
  },
]

const steps = [
  ['Create Avior’s roles, once per server', 'deploy/store/bootstrap.sql prompts for each password. The observatory needs pg_stat_statements loaded.'],
  ['Put every secret in a file', 'Each password and the session secret is mounted from your secret store and named with _FILE.'],
  ['Run the migrate job', 'It applies db/migrations to aviorDB, connecting as its owner rather than as the gateway.'],
  ['Roll out the gateway and the console', 'Two to four gateway replicas; readiness asks /api/v1/health.'],
  ['Route one host', 'The ingress sends /api to the gateway and everything else to the console.'],
] as const

const config = `AVIOR_ENV=production
AVIOR_AUTH_MODE=ldap
AVIOR_LDAP_URL=ldaps://directory.example.com
AVIOR_LDAP_BIND_FORMAT=uid=%s,ou=people,dc=example,dc=com
AVIOR_STORE_DSN=postgres://avior@db:5432/aviordb
AVIOR_TARGET_DSN=postgres://avior_governed@db:5432/postgres
AVIOR_WRITE_DSN=postgres://avior_writer@db:5432/postgres
AVIOR_DEPLOY_DSN=postgres://avior_deployer@db:5432/postgres
AVIOR_PROVISION_DSN=postgres://avior_provisioner@db:5432/postgres
AVIOR_STORE_PASSWORD_FILE=/secrets/avior
AVIOR_SESSION_SECRET_FILE=/secrets/avior_session_secret
AVIOR_ADMIN_PASSWORD_FILE=/secrets/avior_admin_password`

export function Deploy() {
  return (
    <section id="deploy" className="band band-2">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow">Deploy</p>
          <h2 className="section-title">Three images, one PostgreSQL server, every secret a file.</h2>
          <p className="section-lede">
            Built for a Kubernetes cluster. Each image does one job with one credential, so none of them can run
            another’s.
          </p>
        </div>

        <ul className="guard-grid deploy-images">
          {images.map(({ icon: Icon, title, body, detail }) => (
            <li key={title} className="tile">
              <div className="tile-top">
                <span className="tile-icon">
                  <Icon size={22} strokeWidth={1.75} />
                </span>
              </div>
              <h3 className="tile-title">{title}</h3>
              <p className="tile-body">{body}</p>
              <code className="tile-detail">{detail}</code>
            </li>
          ))}
        </ul>

        <div className="deploy">
          <ol className="deploy-steps">
            {steps.map(([title, body], i) => (
              <li key={title} className="pipeline-step">
                <span className="pipeline-n">{i + 1}</span>
                <div>
                  <h3 className="pipeline-title">{title}</h3>
                  <p className="pipeline-body">{body}</p>
                </div>
              </li>
            ))}
          </ol>
          <div className="deploy-config">
            <CodeBlock title="The gateway’s environment" language="bash" code={config} isWrapped />
            <p className="releases-note">
              It refuses to start on a missing value, the development session secret, or self-approval of writes in
              production. Each further password follows the same <code>_FILE</code> pattern.
            </p>
            <div className="releases-actions">
              <Button variant="secondary" size="lg" label="New database runbook" href={links.databaseRunbook} />
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
