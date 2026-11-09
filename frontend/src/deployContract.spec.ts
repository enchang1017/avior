import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * The paths an MCP client probes for OAuth after Avior answers 401, as Claude
 * Code was recorded probing them. Avior has no OAuth (ADR-0007), so each must
 * be a 404 from nginx rather than the SPA's index.html.
 */
const oauthProbes = [
  '/.well-known/oauth-protected-resource',
  '/.well-known/oauth-protected-resource/api/v1/mcp',
  '/.well-known/oauth-authorization-server',
  '/.well-known/openid-configuration',
  '/register',
]

interface Location {
  exact: boolean
  path: string
  body: string
}

function locations(conf: string): Location[] {
  const found: Location[] = []
  for (const match of conf.matchAll(/location\s+(=\s+)?(\S+)\s*\{([^}]*)\}/gu)) {
    found.push({ exact: match[1] !== undefined, path: match[2] ?? '', body: match[3] ?? '' })
  }
  return found
}

function served(all: Location[], path: string): Location | undefined {
  const exact = all.find((l) => l.exact && l.path === path)
  if (exact) return exact
  return all
    .filter((l) => !l.exact && path.startsWith(l.path))
    .sort((a, b) => b.path.length - a.path.length)[0]
}

describe('nginx.conf', () => {
  const conf = readFileSync(join(process.cwd(), 'deploy', 'nginx.conf'), 'utf8')
  const all = locations(conf)

  it.each(oauthProbes)('answers %s with 404 rather than the SPA', (path) => {
    const location = served(all, path)
    expect(location?.path).not.toBe('/')
    expect(location?.body).toMatch(/return\s+404;/u)
  })

  it('still serves the router from index.html', () => {
    expect(served(all, '/account/agent')?.body).toMatch(/try_files .* \/index\.html;/u)
  })
})
