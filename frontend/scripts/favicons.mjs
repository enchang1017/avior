#!/usr/bin/env node
/*
 * The favicons, drawn from src/assets/logo/southward-favicon.svg by the
 * browser the tests already use: favicon.ico at 16, 32 and 48 px, and the
 * 180 px apple-touch-icon (ADR 0009, The logo). Run it when the mark changes;
 * its outputs are committed.
 *
 *   node scripts/favicons.mjs
 */
import { readFileSync, writeFileSync } from 'node:fs'
import { Buffer } from 'node:buffer'
import process from 'node:process'

import { chromium } from '@playwright/test'

const svg = readFileSync('src/assets/logo/southward-favicon.svg', 'utf8')

async function render(browser, size) {
  const page = await browser.newPage({ viewport: { width: size, height: size } })
  await page.setContent(
    `<style>html,body{margin:0;background:transparent}svg{display:block;width:${size}px;height:${size}px}</style>${svg}`,
  )
  const png = await page.screenshot({ omitBackground: true })
  await page.close()
  return png
}

/** An ICO whose images are PNGs, which every browser since IE Vista reads. */
function ico(images) {
  const header = Buffer.alloc(6)
  header.writeUInt16LE(0, 0)
  header.writeUInt16LE(1, 2)
  header.writeUInt16LE(images.length, 4)
  let offset = 6 + 16 * images.length
  const entries = images.map(({ size, png }) => {
    const entry = Buffer.alloc(16)
    entry.writeUInt8(size >= 256 ? 0 : size, 0)
    entry.writeUInt8(size >= 256 ? 0 : size, 1)
    entry.writeUInt16LE(1, 4)
    entry.writeUInt16LE(32, 6)
    entry.writeUInt32LE(png.length, 8)
    entry.writeUInt32LE(offset, 12)
    offset += png.length
    return entry
  })
  return Buffer.concat([header, ...entries, ...images.map(({ png }) => png)])
}

const browser = await chromium.launch()
const images = []
for (const size of [16, 32, 48]) images.push({ size, png: await render(browser, size) })
writeFileSync('public/favicon.ico', ico(images))
writeFileSync('public/apple-touch-icon.png', await render(browser, 180))
await browser.close()
process.stdout.write('wrote public/favicon.ico and public/apple-touch-icon.png\n')
