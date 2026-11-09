import { useEffect, useRef } from 'react'

type Star = { x: number; y: number; z: number }
type Palette = { ground: string; strokes: string[] }

const STAR_POOL = 900
const VISIBLE_STARS = 320
const SPEED = 0.0028
const TRAIL_FADE = 0.35
const NEAREST = 0.02
const REDUCED_MOTION_FRAMES = 400

function randomStar(z = Math.random()): Star {
  return { x: Math.random() * 2 - 1, y: Math.random() * 2 - 1, z }
}

function project(value: number, depth: number, centre: number, focal: number) {
  return centre + (value / depth) * focal * 0.5
}

function readPalette(el: HTMLElement): Palette {
  const style = getComputedStyle(el)
  const read = (name: string) => style.getPropertyValue(name).trim()
  return { ground: read('--color-paper'), strokes: [read('--star-1'), read('--star-2'), read('--star-3')] }
}

function drawFrame(ctx: CanvasRenderingContext2D, stars: Star[], width: number, height: number, palette: Palette) {
  ctx.globalAlpha = TRAIL_FADE
  ctx.fillStyle = palette.ground
  ctx.fillRect(0, 0, width, height)

  const cx = width / 2
  const cy = height / 2
  const focal = Math.max(width, height) * 0.5

  stars.slice(0, VISIBLE_STARS).forEach((star, i) => {
    const previous = star.z
    star.z -= SPEED
    if (star.z <= NEAREST) {
      Object.assign(star, randomStar(1))
      return
    }
    ctx.strokeStyle = palette.strokes[i % palette.strokes.length] ?? palette.ground
    ctx.lineWidth = (1 - star.z) * 2.6
    ctx.globalAlpha = 1 - star.z
    ctx.beginPath()
    ctx.moveTo(project(star.x, previous, cx, focal), project(star.y, previous, cy, focal))
    ctx.lineTo(project(star.x, star.z, cx, focal), project(star.y, star.z, cy, focal))
    ctx.stroke()
  })
  ctx.globalAlpha = 1
}

export function Starfield() {
  const hostRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const host = hostRef.current
    const canvas = canvasRef.current
    const ctx = canvas?.getContext('2d')
    if (!host || !canvas || !ctx) return

    const stars = Array.from({ length: STAR_POOL }, () => randomStar())
    let width = 0
    let height = 0
    let frame = 0
    let visible = true
    let palette = readPalette(host)

    const fit = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      width = host.clientWidth
      height = host.clientHeight
      canvas.width = width * ratio
      canvas.height = height * ratio
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0)
    }

    const loop = () => {
      drawFrame(ctx, stars, width, height, palette)
      frame = visible ? requestAnimationFrame(loop) : 0
    }

    const resize = new ResizeObserver(fit)
    resize.observe(host)
    fit()

    const settle = () => Array.from({ length: REDUCED_MOTION_FRAMES }).forEach(() => drawFrame(ctx, stars, width, height, palette))
    const recolour = new MutationObserver(() => {
      palette = readPalette(host)
      if (reduced) settle()
    })
    recolour.observe(document.documentElement, { attributes: true, attributeFilter: ['data-mode'] })
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches

    if (reduced) {
      settle()
      return () => {
        resize.disconnect()
        recolour.disconnect()
      }
    }

    // The hero scrolls away on a long page; a canvas painting off-screen is a
    // laptop fan for nothing, so the loop stops while it cannot be seen.
    const seen = new IntersectionObserver(([entry]) => {
      visible = entry?.isIntersecting ?? false
      if (visible && frame === 0) frame = requestAnimationFrame(loop)
    })
    seen.observe(host)
    frame = requestAnimationFrame(loop)

    return () => {
      cancelAnimationFrame(frame)
      resize.disconnect()
      recolour.disconnect()
      seen.disconnect()
    }
  }, [])

  return (
    <div ref={hostRef} className="starfield" aria-hidden="true">
      <canvas ref={canvasRef} />
    </div>
  )
}
