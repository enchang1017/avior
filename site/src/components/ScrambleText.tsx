import { Fragment, useCallback, useEffect, useRef, useState } from 'react'

import { brand } from '../brand'

type ScrambleTextProps = {
  text: string
  duration?: number
}

type Glyphs = (string | null)[]

const NARROW = 'il1!|:;/'
const UPPER = 'ABDEFHKLNPRSTUXZ'
const LOWER = 'abcdeghknopqsuvxyz#*'
const REPAINT_MS = 50

function glyphSet(c: string) {
  if (/[iljtfr.,;:!'|]/.test(c)) return NARROW
  return /[A-Z]/.test(c) ? UPPER : LOWER
}

function glyph(c: string, n: number) {
  const set = glyphSet(c)
  return set.charAt(Math.floor(n * set.length) % set.length)
}

function noise(text: string): Glyphs {
  return [...text].map((c, i) => (c === ' ' ? null : glyph(c, ((i * 7 + 3) % 10) / 10)))
}

function prefersReducedMotion() {
  return matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function ScrambleText({ text, duration = 1600 }: ScrambleTextProps) {
  const ref = useRef<HTMLSpanElement>(null)
  const frame = useRef(0)
  const [glyphs, setGlyphs] = useState<Glyphs>(() => noise(text))

  const decode = useCallback(() => {
    cancelAnimationFrame(frame.current)
    const chars = [...text]
    const locks = chars.map((_, i) => (i / chars.length) * 0.75 + Math.random() * 0.25)
    const reduce = prefersReducedMotion()
    const start = performance.now()
    let last = 0

    const step = (now: number) => {
      const progress = reduce ? 1 : Math.min(1, (now - start) / duration)
      if (now - last > REPAINT_MS || progress === 1) {
        last = now
        setGlyphs(chars.map((c, i) => (c === ' ' || progress >= (locks[i] ?? 0) ? null : glyph(c, Math.random()))))
      }
      if (progress < 1) frame.current = requestAnimationFrame(step)
    }
    frame.current = requestAnimationFrame(step)
  }, [text, duration])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const seen = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return
        decode()
        seen.disconnect()
      },
      { threshold: 0.5 },
    )
    seen.observe(el)
    return () => {
      seen.disconnect()
      cancelAnimationFrame(frame.current)
    }
  }, [decode])

  const words = text.split(' ')
  const offsets = words.reduce<number[]>((acc, _, i) => [...acc, i === 0 ? 0 : (acc[i - 1] ?? 0) + (words[i - 1]?.length ?? 0) + 1], [])

  return (
    <span ref={ref} className="scramble" aria-label={text} onClick={decode}>
      <span aria-hidden="true">
        {words.map((word, wi) => (
          <Fragment key={wi}>
            <span className="scramble-word">
              {[...word].map((ch, ci) => {
                const g = glyphs[(offsets[wi] ?? 0) + ci]
                return (
                  <span key={ci} className="scramble-cell">
                    <span className="scramble-char" style={{ opacity: g ? 0 : 1 }}>
                      {ch}
                    </span>
                    {g ? (
                      <span className="scramble-glyph" style={{ color: brand.amber }}>
                        {g}
                      </span>
                    ) : null}
                  </span>
                )
              })}
            </span>
            {wi < words.length - 1 ? ' ' : null}
          </Fragment>
        ))}
      </span>
    </span>
  )
}
