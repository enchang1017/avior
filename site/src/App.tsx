import { Agents } from './sections/Agents'
import { Features } from './sections/Features'
import { Footer } from './sections/Footer'
import { Guard } from './sections/Guard'
import { Harness } from './sections/Harness'
import { Hero } from './sections/Hero'
import { Nav } from './sections/Nav'
import { Start } from './sections/Start'

export function App() {
  return (
    <div id="top">
      <Nav />
      <Hero />
      <main>
        <Guard />
        <Features />
        <Agents />
        <Harness />
        <Start />
      </main>
      <Footer />
    </div>
  )
}
