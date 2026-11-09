import { useEffect, useRef, useState } from 'react'

type Stage = 'parse' | 'plan' | 'audit' | 'run'
type Verdict = 'allow' | 'deny'

type Case = {
  sql: string
  verdict: Verdict
  stoppedAt: Stage
  note: string
}

const STAGES: Stage[] = ['parse', 'plan', 'audit', 'run']

const CASES: Case[] = [
  {
    sql: 'SELECT id, region, created_at FROM players WHERE region = $1 LIMIT 50',
    verdict: 'allow',
    stoppedAt: 'run',
    note: 'read-only transaction · statement, lock and idle timeout set',
  },
  {
    sql: 'DELETE FROM players WHERE created_at < now() - interval $1',
    verdict: 'deny',
    stoppedAt: 'parse',
    note: 'only SELECT and WITH may lead — request a governed write instead',
  },
  {
    sql: 'SELECT * FROM player_contact_view WHERE id = $1',
    verdict: 'deny',
    stoppedAt: 'plan',
    note: 'the plan reads players.email, a restricted column, through the view',
  },
  {
    sql: 'SELECT refresh_balances()',
    verdict: 'deny',
    stoppedAt: 'parse',
    note: 'a routine outside pg_catalog is refused rather than guessed at',
  },
]

const TYPE_MS = 22
const STAGE_MS = 340
const HOLD_MS = 2600

type Frame = { index: number; typed: number; stage: number; done: boolean }

const START: Frame = { index: 0, typed: 0, stage: -1, done: false }

function stageState(c: Case, frame: Frame, i: number): 'idle' | 'pass' | 'fail' | 'skip' {
  const stop = STAGES.indexOf(c.stoppedAt)
  if (frame.stage < i) return 'idle'
  if (c.verdict === 'deny' && i === stop) return 'fail'
  if (STAGES[i] === 'audit') return 'pass'
  if (c.verdict === 'deny' && i > stop) return 'skip'
  return 'pass'
}

function lastStage(c: Case) {
  return c.verdict === 'deny' ? STAGES.length - 1 : STAGES.indexOf(c.stoppedAt)
}

function nextFrame(frame: Frame): { frame: Frame; wait: number } {
  const c = CASES[frame.index] ?? CASES[0]
  if (!c) return { frame: START, wait: HOLD_MS }
  if (frame.typed < c.sql.length) return { frame: { ...frame, typed: frame.typed + 1 }, wait: TYPE_MS }
  if (frame.stage < lastStage(c)) return { frame: { ...frame, stage: frame.stage + 1 }, wait: STAGE_MS }
  if (!frame.done) return { frame: { ...frame, done: true }, wait: HOLD_MS }
  return { frame: { ...START, index: (frame.index + 1) % CASES.length }, wait: TYPE_MS }
}

function finished(index: number): Frame {
  const c = CASES[index]
  return { index, typed: c?.sql.length ?? 0, stage: c ? lastStage(c) : 0, done: true }
}

export function GuardConsole() {
  const ref = useRef<HTMLDivElement>(null)
  const [frame, setFrame] = useState<Frame>(START)
  const [running, setRunning] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setFrame(finished(2))
      return
    }
    const seen = new IntersectionObserver(([entry]) => setRunning(entry?.isIntersecting ?? false))
    seen.observe(el)
    return () => seen.disconnect()
  }, [])

  useEffect(() => {
    if (!running) return
    const { frame: next, wait } = nextFrame(frame)
    const timer = window.setTimeout(() => setFrame(next), wait)
    return () => window.clearTimeout(timer)
  }, [running, frame])

  const c = CASES[frame.index] ?? CASES[0]
  if (!c) return null

  return (
    <div ref={ref} className="console" role="img" aria-label="A statement passing through Avior's guard: parse, plan, audit, run.">
      <div className="console-head" aria-hidden="true">
        <span className="console-target">
          <span className="console-dot" /> production · aviorfixture
        </span>
        <span className="console-step">
          {frame.index + 1} / {CASES.length}
        </span>
      </div>

      <pre className="console-sql" aria-hidden="true">
        <span className="console-prompt">avior=&gt;</span> {c.sql.slice(0, frame.typed)}
        <span className={frame.typed < c.sql.length ? 'console-caret' : 'console-caret is-idle'} />
      </pre>

      <ol className="console-stages" aria-hidden="true">
        {STAGES.map((s, i) => (
          <li key={s} className="console-stage" data-state={stageState(c, frame, i)}>
            <span className="console-stage-mark" />
            {s}
          </li>
        ))}
      </ol>

      <div className="console-verdict" data-verdict={c.verdict} data-shown={frame.done} aria-hidden="true">
        <span className="console-verdict-word">{c.verdict}</span>
        <span className="console-verdict-note">{c.note}</span>
      </div>
    </div>
  )
}
