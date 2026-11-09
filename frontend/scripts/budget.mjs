#!/usr/bin/env node
/*
 * ADR 0009 P1 to P7: what the build ships, against the budget, on the build's
 * own manifest. Run after `vite build`; exits non-zero on any row over.
 *
 *   node scripts/budget.mjs              check dist/
 *   node scripts/budget.mjs --self-test  prove every check can fail
 *
 * A route's cost is the static import closure of its chunks, gzipped at level
 * 9: what the browser must fetch before that page can draw. Lazy chunks are
 * held to their own limits and may sit in no route's closure.
 */
import {
  existsSync,
  mkdtempSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'
import { Buffer } from 'node:buffer'
import process from 'node:process'

const say = (line) => process.stdout.write(`${line}\n`)

export const limits = {
  shellKb: 60,
  consoleKb: 200,
  routeKb: 90,
  lazyKb: { palette: 8, chart: 12, sky: 6, vim: 65 },
  cssKb: 16,
  fontKb: 24,
  dependencyKb: 10,
}

const entry = 'index.html'
const shell = 'src/layouts/AppShell.vue'
const console_ = 'src/features/query/QueryConsoleView.vue'
const signIn = 'src/features/auth/LoginView.vue'

/**
 * Dependencies the console already shipped before the redesign; P7 holds only
 * what came after. `@codemirror/commands` and `@codemirror/search` are named
 * directly now, where `codemirror`'s basic setup used to pull them in, so they
 * add nothing the build did not already carry.
 */
const before = new Set([
  '@codemirror/autocomplete',
  '@codemirror/commands',
  '@codemirror/lang-sql',
  '@codemirror/language',
  '@codemirror/search',
  '@codemirror/state',
  '@codemirror/view',
  '@lezer/highlight',
  '@replit/codemirror-vim',
  'codemirror',
  'openapi-fetch',
  'pinia',
  'vue',
  'vue-router',
])

const lazy = {
  palette: (key) =>
    key.endsWith('components/Overlay/VPalette/VPalette.vue') || key.includes('features/search/'),
  chart: (key) => key.includes('VSchemaChart'),
  sky: (key) => key.includes('VSkyline') || key.includes('AuditSky'),
  vim: (key) => key.includes('@replit/codemirror-vim') || key.includes('@replit+codemirror-vim'),
}

const kb = (bytes) => Math.round((bytes / 1024) * 10) / 10

function gz(dir, file) {
  const bytes = readFileSync(join(dir, file))
  return /\.(woff2?|png|ico|jpe?g|webp|avif)$/u.test(file)
    ? bytes.length
    : gzipSync(bytes, { level: 9 }).length
}

function closure(manifest, keys) {
  const js = new Set()
  const seen = new Set()
  const visit = (key) => {
    if (seen.has(key)) return
    seen.add(key)
    const chunk = manifest[key]
    if (chunk === undefined) throw new Error(`the build has no chunk for ${key}`)
    js.add(chunk.file)
    for (const imported of chunk.imports ?? []) visit(imported)
  }
  keys.forEach(visit)
  return js
}

const sum = (dir, files) => [...files].reduce((total, file) => total + gz(dir, file), 0)

/**
 * Every routed view. A view whose chunk another lazy chunk imports from is keyed
 * by its hashed file, `_LineageView-abc.js`, not its source, so it is found by
 * its chunk's name as well; keyed by source alone it would drop out of P3 the
 * moment a page split a panel off, and pass by not being measured.
 */
function routes(manifest) {
  return Object.entries(manifest)
    .filter(
      ([key, chunk]) =>
        chunk.isDynamicEntry === true &&
        (key.endsWith('View.vue') || (key.startsWith('_') && /View$/u.test(chunk.name ?? ''))),
    )
    .map(([key]) => key)
}

const routeName = (manifest, view) =>
  view.startsWith('_')
    ? `${manifest[view]?.name ?? view}.vue`
    : view.replace(/^src\/features\//u, '')

function routeRows(dir, manifest) {
  const rows = [
    {
      row: 'P1 shell',
      what: 'entry + shell',
      bytes: sum(dir, closure(manifest, [entry, shell])),
      limit: limits.shellKb,
    },
  ]
  for (const view of routes(manifest)) {
    const keys = view === signIn ? [entry, view] : [entry, shell, view]
    const isConsole = view === console_
    rows.push({
      row: isConsole ? 'P2 console' : 'P3 route',
      what: routeName(manifest, view),
      bytes: sum(dir, closure(manifest, keys)),
      limit: isConsole ? limits.consoleKb : limits.routeKb,
    })
  }
  return rows
}

function lazyRows(dir, manifest) {
  const rows = []
  const everyRoute = new Set(
    [[entry, shell], ...routes(manifest).map((view) => [entry, shell, view])].flatMap((keys) => [
      ...closure(manifest, keys),
    ]),
  )
  for (const [name, matches] of Object.entries(lazy)) {
    for (const [key, chunk] of Object.entries(manifest)) {
      if (!matches(key) || chunk.isDynamicEntry !== true) continue
      const own = [...closure(manifest, [key])].filter(
        (file) => !everyRoute.has(file) || file === chunk.file,
      )
      const leaked = everyRoute.has(chunk.file)
      rows.push({
        row: 'P4 lazy',
        what: `${name}: ${key.split('/').pop()}`,
        bytes: leaked ? Infinity : sum(dir, own),
        limit: limits.lazyKb[name],
      })
    }
  }
  return rows
}

function assets(dir) {
  const folder = join(dir, 'assets')
  return existsSync(folder) ? readdirSync(folder).map((file) => `assets/${file}`) : []
}

function styleAndFontRows(dir) {
  const files = assets(dir)
  const css = files.filter((file) => file.endsWith('.css'))
  const fonts = files.filter(
    (file) =>
      /\.(woff2?|ttf|otf|eot)$/u.test(file) || (/\.svg$/u.test(file) && file.includes('font')),
  )
  const rows = [
    {
      row: 'P5 css',
      what: `${css.length} stylesheet(s)`,
      bytes: sum(dir, css),
      limit: limits.cssKb,
    },
  ]
  const one = fonts.length === 1 && fonts[0].endsWith('.woff2')
  rows.push({
    row: 'P6 fonts',
    what: fonts.map((f) => f.split('/').pop()).join(', ') || 'none',
    bytes: one ? sum(dir, fonts) : Infinity,
    limit: limits.fontKb,
  })
  const html = existsSync(join(dir, 'index.html'))
    ? readFileSync(join(dir, 'index.html'), 'utf8')
    : ''
  const preloaded = one && html.includes(`rel="preload" href="/${fonts[0]}" as="font"`)
  rows.push({
    row: 'P6 preload',
    what: 'the font is preloaded',
    bytes: preloaded ? 0 : Infinity,
    limit: 0,
  })
  return rows
}

function dependencyRows(dir, packageJson, lock) {
  const graphFile = join(dir, '.vite', 'modules.json')
  const graph = existsSync(graphFile) ? JSON.parse(readFileSync(graphFile, 'utf8')) : {}
  const dependencies = packageJson.dependencies ?? {}
  const rows = []
  for (const [name, version] of Object.entries(dependencies)) {
    if (!/^\d+\.\d+\.\d+/u.test(version))
      rows.push({ row: 'P7 pinned', what: `${name}@${version}`, bytes: Infinity, limit: 0 })
    if (before.has(name)) continue
    let worst = 0
    for (const byPackage of Object.values(graph)) {
      const code = byPackage[name]
      if (code) worst = Math.max(worst, gzipSync(Buffer.from(code), { level: 9 }).length)
    }
    rows.push({ row: 'P7 dependency', what: name, bytes: worst, limit: limits.dependencyKb })
  }
  if (/primeicons/u.test(lock))
    rows.push({
      row: 'P6 icon font',
      what: 'primeicons in the lockfile',
      bytes: Infinity,
      limit: 0,
    })
  return rows
}

export function measure(dir, packageJson, lock) {
  const manifest = JSON.parse(readFileSync(join(dir, '.vite', 'manifest.json'), 'utf8'))
  return [
    ...routeRows(dir, manifest),
    ...lazyRows(dir, manifest),
    ...styleAndFontRows(dir),
    ...dependencyRows(dir, packageJson, lock),
  ]
}

const isOver = (row) => row.bytes > row.limit * 1024

export const over = (rows) => rows.filter(isOver)

function print(rows) {
  for (const row of rows) {
    const mark = isOver(row) ? 'OVER' : 'ok  '
    const shown = row.bytes === Infinity ? 'fails' : `${kb(row.bytes)} kB`
    say(
      `${mark}  ${row.row.padEnd(14)} ${shown.padStart(9)} / ${String(row.limit).padStart(3)} kB  ${row.what}`,
    )
  }
}

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), 'avior-budget-'))
  mkdirSync(join(dir, '.vite'))
  mkdirSync(join(dir, 'assets'))
  const random = (bytes) =>
    Buffer.from(Array.from({ length: bytes }, () => Math.floor(Math.random() * 256)))
  const write = (file, bytes) => writeFileSync(join(dir, file), random(bytes))
  write('assets/entry.js', 1024)
  write('assets/shell.js', 1024)
  write('assets/fat.js', (limits.routeKb + 5) * 1024)
  write('assets/palette.js', 512)
  write('assets/a.woff2', 1024)
  write('assets/b.woff', 1024)
  writeFileSync(join(dir, 'index.html'), '<link rel="preload" href="/assets/a.woff2" as="font">')
  const manifest = {
    'index.html': { file: 'assets/entry.js', isEntry: true },
    [shell]: { file: 'assets/shell.js', isDynamicEntry: true, imports: ['index.html'] },
    // Keyed as a view is once a lazy panel imports from its chunk.
    '_FatView-abc.js': {
      file: 'assets/fat.js',
      name: 'FatView',
      isDynamicEntry: true,
      imports: ['index.html'],
    },
    'src/components/Overlay/VPalette/VPalette.vue': {
      file: 'assets/shell.js',
      isDynamicEntry: true,
    },
  }
  writeFileSync(join(dir, '.vite', 'manifest.json'), JSON.stringify(manifest))
  writeFileSync(
    join(dir, '.vite', 'modules.json'),
    JSON.stringify({
      'assets/fat.js': { 'fat-dep': 'x'.repeat(1) + random(20 * 1024).toString('base64') },
    }),
  )
  const rows = over(
    measure(dir, { dependencies: { vue: '3.5.13', 'fat-dep': '^1.0.0' } }, 'primeicons@7.0.0'),
  )
  const found = new Set(rows.map((row) => row.row))
  const expected = [
    'P3 route',
    'P4 lazy',
    'P6 fonts',
    'P6 preload',
    'P7 pinned',
    'P7 dependency',
    'P6 icon font',
  ]
  const missing = expected.filter((row) => !found.has(row))
  if (missing.length > 0) {
    console.error(`FAIL  the budget could not tell a violation of ${missing.join(', ')}`)
    process.exit(1)
  }
  checkTheByteEdge()
  say(`PASS  budget self-test: ${expected.length} kinds of violation each caught`)
}

function checkTheByteEdge() {
  const limit = limits.shellKb
  const at = { row: 'P1 shell', what: 'at the limit', bytes: limit * 1024, limit }
  const past = { ...at, what: 'one byte past it', bytes: limit * 1024 + 1 }
  if (over([at]).length !== 0 || over([past]).length !== 1) {
    console.error(`FAIL  the budget does not draw its line at exactly ${limit} kB`)
    process.exit(1)
  }
  say(`PASS  budget self-test: ${limit} kB passes and one byte more fails`)
}

if (process.argv.includes('--self-test')) {
  selfTest()
} else {
  const rows = measure(
    'dist',
    JSON.parse(readFileSync('package.json', 'utf8')),
    readFileSync('pnpm-lock.yaml', 'utf8'),
  )
  print(rows)
  const failed = over(rows)
  if (failed.length > 0) {
    console.error(`\nFAIL  ${failed.length} row(s) over ADR 0009's budget`)
    process.exit(1)
  }
  say('\nPASS  ADR 0009 P1 to P7')
}
