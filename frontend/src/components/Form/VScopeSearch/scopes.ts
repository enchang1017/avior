/**
 * A search box's query read as scopes and words: `database:aviorfixture bob`
 * is one chip and one word. Only the keys a page names are scopes; anything
 * else, colon or not, is a word, so a statement fragment like `a:b` still
 * searches as typed.
 */
export type Scope = { key: string; value: string }
export type ParsedQuery = { scopes: Scope[]; words: string[] }

export function parseQuery(query: string, keys: readonly string[]): ParsedQuery {
  const scopes: Scope[] = []
  const words: string[] = []
  for (const token of query.trim().split(/\s+/u).filter(Boolean)) {
    const at = token.indexOf(':')
    const key = at > 0 ? token.slice(0, at).toLowerCase() : ''
    const value = at > 0 ? token.slice(at + 1) : ''
    if (keys.includes(key) && value !== '') scopes.push({ key, value })
    else words.push(token.toLowerCase())
  }
  return { scopes, words }
}

export function formatQuery(parsed: ParsedQuery): string {
  return [...parsed.scopes.map((scope) => `${scope.key}:${scope.value}`), ...parsed.words].join(' ')
}

export function withoutScope(query: string, keys: readonly string[], index: number): string {
  const parsed = parseQuery(query, keys)
  return formatQuery({ ...parsed, scopes: parsed.scopes.filter((_, at) => at !== index) })
}
