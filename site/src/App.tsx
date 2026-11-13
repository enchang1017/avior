import { Theme } from '@astryxdesign/core/theme'

import { useColorMode } from './colorMode'
import { Agents } from './sections/Agents'
import { Architecture } from './sections/Architecture'
import { Deploy } from './sections/Deploy'
import { Facts } from './sections/Facts'
import { Faq } from './sections/Faq'
import { Features } from './sections/Features'
import { Footer } from './sections/Footer'
import { Guard } from './sections/Guard'
import { Hero } from './sections/Hero'
import { Nav } from './sections/Nav'
import { Refusals } from './sections/Refusals'
import { Releases } from './sections/Releases'
import { Start } from './sections/Start'
import { Writes } from './sections/Writes'
import { aviorTheme } from './theme'

export function App() {
  const { mode, resolved, setMode } = useColorMode()

  return (
    <Theme theme={aviorTheme} mode={resolved}>
      <div id="top">
        <Nav mode={mode} onModeChange={setMode} />
        <Hero />
        <main>
          <Refusals />
          <Guard />
          <Writes />
          <Features />
          <Architecture />
          <Releases />
          <Agents />
          <Facts />
          <Faq />
          <Deploy />
          <Start />
        </main>
        <Footer />
      </div>
    </Theme>
  )
}
