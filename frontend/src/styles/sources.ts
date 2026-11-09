import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { parse } from 'vue/compiler-sfc'

/*
 * The parts of a source file the style gates read, taken apart once.
 *
 * Each gate asks about a different part — the classes a template names, the
 * values a script hands to one, what a `<style>` block declares — so this reads
 * a file into those parts and the gates stay a list of rules.
 */

/*
 * The shape of the template AST this needs, declared here rather than imported.
 *
 * `vue/compiler-sfc` re-exports the parser and not its node types, and pulling
 * @vue/compiler-dom in for four fields would add a dependency to read a
 * dependency's output. These are the four: the node kind, the tag, its props
 * and its children.
 */
type Attribute = { type: 6; name: string; value?: { content: string } }
type Directive = {
  type: 7
  name: string
  arg?: { type: number; content?: string }
  exp?: { content?: string }
}
export type Prop = Attribute | Directive
export type TemplateNode = {
  type: number
  tag?: string
  props?: Prop[]
  children?: TemplateNode[]
  loc: { start: { line: number } }
}

export type Source = {
  file: string
  /** Top-level template nodes, empty for a script file. */
  template: TemplateNode[]
  /** Script bodies, for a component its `<script>` blocks; for a module the file. */
  scripts: string[]
  /** `<style>` block bodies. */
  styles: string[]
}

export function files(dir: string, pattern: RegExp, found: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) {
      if (entry !== 'gen' && entry !== '__tests__') files(path, pattern, found)
      continue
    }
    if (pattern.test(entry) && !entry.endsWith('.spec.ts')) found.push(path)
  }
  return found
}

export function read(file: string, text = readFileSync(file, 'utf8')): Source {
  if (!file.endsWith('.vue')) return { file, template: [], scripts: [text], styles: [] }
  // The gates read line numbers off the AST; a source map per block is work nobody reads.
  const { descriptor, errors } = parse(text, { filename: file, sourceMap: false })
  if (errors.length > 0) throw new Error(`${file} does not parse: ${String(errors[0])}`)
  return {
    file,
    template: (descriptor.template?.ast?.children ?? []) as unknown as TemplateNode[],
    scripts: [descriptor.script?.content ?? '', descriptor.scriptSetup?.content ?? ''],
    styles: descriptor.styles.map((style) => style.content),
  }
}

export function each(nodes: TemplateNode[], visit: (node: TemplateNode) => void): void {
  for (const node of nodes) {
    // 1 is an element. Text, interpolation and comments carry no attributes.
    if (node.type !== 1) continue
    visit(node)
    each(node.children ?? [], visit)
  }
}

/** The static value of an attribute, or empty when it is bound or absent. */
export function staticAttr(node: TemplateNode, name: string): string {
  for (const prop of node.props ?? []) {
    if (prop.type === 6 && prop.name === name) return prop.value?.content ?? ''
  }
  return ''
}

/** The expression bound to an attribute with `:name`, or empty. */
export function boundAttr(node: TemplateNode, name: string): string {
  for (const prop of node.props ?? []) {
    if (prop.type === 7 && prop.name === 'bind' && prop.arg?.content === name) {
      return prop.exp?.content ?? ''
    }
  }
  return ''
}

export function hasBoundAttr(node: TemplateNode, name: string): boolean {
  return (node.props ?? []).some(
    (prop) => prop.type === 7 && prop.name === 'bind' && prop.arg?.content === name,
  )
}

/**
 * Every quoted string in a piece of code: where a script keeps the classes it
 * hands out. Comments go first, since they quote code in backticks and a quoted
 * example of a mistake is not the mistake.
 */
export function literals(code: string): string[] {
  const bare = code.replace(/\/\*[\s\S]*?\*\//gu, '').replace(/^\s*\/\/.*$/gmu, '')
  return [...bare.matchAll(/'([^'\n]*)'|"([^"\n]*)"|`([^`]*)`/gu)].map(
    (found) => found[1] ?? found[2] ?? found[3] ?? '',
  )
}

/** Every class a template names on an element, statically or inside a bound expression. */
export function classesOf(node: TemplateNode): string[] {
  const named = [staticAttr(node, 'class'), ...literals(boundAttr(node, 'class'))]
  return named.flatMap((list) => list.split(/\s+/u)).filter(Boolean)
}
