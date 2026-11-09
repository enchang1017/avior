import { Grid } from '@astryxdesign/core/Grid'
import { Heading } from '@astryxdesign/core/Heading'
import { Link } from '@astryxdesign/core/Link'
import { Text } from '@astryxdesign/core/Text'
import { VStack } from '@astryxdesign/core/VStack'

import { links } from '../brand'

const features = [
  {
    title: 'Governed writes',
    body: 'A developer requests an UPDATE or DELETE. An approver reads what it would touch and how many rows, then lets it run. Every write keeps what it overwrote, in the same transaction.',
  },
  {
    title: 'Column restrictions',
    body: 'A restricted column is refused to everybody, including a DBA, until somebody unlocks it for a role and says why.',
  },
  {
    title: 'Database access by role',
    body: 'Which roles reach which governed database — refused on every path, rather than merely hidden from a list.',
  },
  {
    title: 'Rate limiting per caller',
    body: 'A statement budget per account, shared by every token and replica, so one caller cannot saturate an instance.',
  },
  {
    title: 'Directory sign-in',
    body: 'Operators authenticate against LDAP. Accounts a deployment declares in a file always work, so a directory outage is not a lockout.',
  },
  {
    title: 'Audit sky',
    body: 'Seven days of submissions as hourly bars above the audit log, allowed and refused apart, with a brush that narrows the log.',
  },
] as const

export function Features() {
  return (
    <section id="features" className="band band-raised">
      <div className="container">
        <VStack gap={8}>
          <VStack gap={3} maxWidth={720}>
            <Text type="label" color="accent">
              What else it governs
            </Text>
            <Heading level={2} type="display-3" textWrap="balance">
              The rules a production database needs, written down before they were built.
            </Heading>
            <Text type="large" color="secondary" as="p">
              Every feature starts as an acceptance spec in <Link href={links.features}>docs/features</Link>,
              and each acceptance criterion has an integration test against a real PostgreSQL.
            </Text>
          </VStack>
          <Grid columns={{ minWidth: 280 }} gap={8}>
            {features.map((f) => (
              <VStack key={f.title} gap={2} hAlign="start">
                <span className="feature-rule" />
                <Heading level={3}>{f.title}</Heading>
                <Text color="secondary" as="p">
                  {f.body}
                </Text>
              </VStack>
            ))}
          </Grid>
        </VStack>
      </div>
    </section>
  )
}
