import { Agents } from './sections/Agents'
import { Facts } from './sections/Facts'
import { Faq } from './sections/Faq'
import { Features } from './sections/Features'
import { Footer } from './sections/Footer'
import { Guard } from './sections/Guard'
import { Hero } from './sections/Hero'
import { Nav } from './sections/Nav'
import { Refusals } from './sections/Refusals'
import { Start } from './sections/Start'
import { Writes } from './sections/Writes'

export function App() {
  return (
    <div id="top">
      <Nav />
      <Hero />
      <main>
        <Refusals />
        <Guard />
        <Writes />
        <Features />
        <Agents />
        <Facts />
        <Faq />
        <Start />
      </main>
      <Footer />
    </div>
  )
}
