import { CommandPalette } from '@astryxdesign/core/CommandPalette'
import { Kbd } from '@astryxdesign/core/Kbd'
import { createStaticSource } from '@astryxdesign/core/Typeahead'
import type { SearchableItem } from '@astryxdesign/core/Typeahead'
import { ArrowUpRight, Hash, MessageCircleQuestion, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { links } from '../brand'
import { questions } from '../content/faq'

type Group = 'On this page' | 'Questions' | 'Docs'
type Entry = SearchableItem<{ group: Group; href: string; hint?: string }>

const pageEntries: Entry[] = [
  ['guard', 'The guard: parse, plan, audit, run', '#guard'],
  ['writes', 'Governed writes', '#writes'],
  ['features', 'Policy: columns, roles, rate limits', '#features'],
  ['architecture', 'Architecture: callers, gateway, databases', '#architecture'],
  ['releases', 'Schema releases from a database project', '#releases'],
  ['agents', 'Agents and the MCP endpoint', '#agents'],
  ['faq', 'Questions', '#faq'],
  ['deploy', 'Deploy to production', '#deploy'],
  ['start', 'Run it locally', '#start'],
].map(([id, label, href]) => ({ id: `page:${id}`, label: label ?? '', auxiliaryData: { group: 'On this page', href: href ?? '#top' } }))

const questionEntries: Entry[] = questions.map(([q], i) => ({
  id: `faq:${i}`,
  label: q,
  auxiliaryData: { group: 'Questions', href: '#faq' },
}))

const docEntries: Entry[] = [
  ['readme', 'README', links.readme, 'github.com'],
  ['features', 'Feature specs', links.features, 'docs/features'],
  ['adr', 'Decision records', links.adr, 'docs/adr'],
  ['postgres', 'Why PostgreSQL only', links.postgresAdr, 'ADR 0005'],
  ['release-runbook', 'Releasing a database project', links.releaseRunbook, 'docs/runbooks'],
  ['database-runbook', 'Bringing a new database under Avior', links.databaseRunbook, 'docs/runbooks'],
  ['release-kit', 'The release kit', links.releaseKit, 'tools/release-kit'],
  ['repo', 'Source on GitHub', links.repo, 'github.com'],
  ['license', 'MIT License', links.license, 'LICENSE'],
].map(([id, label, href, hint]) => ({
  id: `doc:${id}`,
  label: label ?? '',
  auxiliaryData: { group: 'Docs', href: href ?? links.repo, hint },
}))

const entries = [...pageEntries, ...questionEntries, ...docEntries]

function icon(group: Group) {
  if (group === 'Docs') return <ArrowUpRight size={16} strokeWidth={1.75} />
  if (group === 'Questions') return <MessageCircleQuestion size={16} strokeWidth={1.75} />
  return <Hash size={16} strokeWidth={1.75} />
}

function go(href: string) {
  if (href.startsWith('#')) {
    document.querySelector(href)?.scrollIntoView({ behavior: 'smooth' })
    history.replaceState(null, '', href)
    return
  }
  window.location.assign(href)
}

export function SiteSearch() {
  const [isOpen, setIsOpen] = useState(false)
  const source = useMemo(() => createStaticSource(entries, { keywords: (e) => [e.auxiliaryData?.group ?? ''] }), [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setIsOpen((open) => !open)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const select = useCallback((id: string) => {
    const entry = entries.find((e) => e.id === id)
    setIsOpen(false)
    if (entry?.auxiliaryData) go(entry.auxiliaryData.href)
  }, [])

  useEffect(() => {
    if (!isOpen) return
    // Astryx's palette ignores Enter until an arrow key has highlighted a row,
    // so typing a query and pressing Enter would do nothing. Take the first
    // result in that case, which is what every launcher-style search does.
    const onEnter = (e: KeyboardEvent) => {
      if (e.key !== 'Enter' || !(e.target instanceof HTMLInputElement)) return
      if (e.target.getAttribute('aria-activedescendant')) return
      const first = document.querySelector<HTMLElement>('dialog[open] [role="option"][data-value]')?.dataset.value
      if (!first) return
      e.preventDefault()
      e.stopPropagation()
      select(first)
    }
    document.addEventListener('keydown', onEnter, true)
    return () => document.removeEventListener('keydown', onEnter, true)
  }, [isOpen, select])

  return (
    <>
      <button type="button" className="search-pill" onClick={() => setIsOpen(true)} aria-label="Search the site">
        <Search size={16} strokeWidth={1.75} aria-hidden="true" />
        <span className="search-pill-label">Search</span>
        <span className="search-pill-keys" aria-hidden="true">
          <Kbd keys="mod+k" />
        </span>
      </button>
      <CommandPalette<Entry>
        isOpen={isOpen}
        onOpenChange={setIsOpen}
        searchSource={source}
        label="Search the site"
        emptyBootstrapText="Search sections, questions and docs"
        onValueChange={select}
        renderItem={(entry) => (
          <span className="search-item">
            <span className="search-item-icon">{icon(entry.auxiliaryData?.group ?? 'On this page')}</span>
            <span className="search-item-label">{entry.label}</span>
            {entry.auxiliaryData?.hint ? <span className="search-item-hint">{entry.auxiliaryData.hint}</span> : null}
          </span>
        )}
      />
    </>
  )
}
