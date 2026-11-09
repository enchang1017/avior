import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

/*
 * The one web font is preloaded, so it arrives with the stylesheet rather than
 * after it (ADR 0009 P6). Its file name is the build's, known only once the
 * bundle is written, so the link is added then.
 */
function preloadMono(): Plugin {
  return {
    name: 'avior-preload-mono',
    transformIndexHtml: {
      order: 'post',
      handler(_html, context) {
        const font = Object.keys(context.bundle ?? {}).find((name) =>
          /jetbrains-mono-latin-400-normal.*\.woff2$/u.test(name),
        )
        if (font === undefined) return []
        return [
          {
            tag: 'link',
            attrs: {
              rel: 'preload',
              href: `/${font}`,
              as: 'font',
              type: 'font/woff2',
              crossorigin: '',
            },
            injectTo: 'head',
          },
        ]
      },
    },
  }
}

/*
 * Which package's code went into which chunk, and how much of it, for the
 * dependency budget (ADR 0009 P7): it counts what a dependency adds to the
 * build, not its published size.
 */
function moduleGraph(): Plugin {
  return {
    name: 'avior-module-graph',
    apply: 'build',
    generateBundle(_options, bundle) {
      const chunks: Record<string, Record<string, string>> = {}
      for (const [file, output] of Object.entries(bundle)) {
        if (output.type !== 'chunk') continue
        const byPackage: Record<string, string> = {}
        for (const [id, rendered] of Object.entries(output.modules)) {
          const found =
            /node_modules\/(?:\.pnpm\/[^/]+\/node_modules\/)?((?:@[^/]+\/)?[^/]+)/u.exec(id)
          const owner = found?.[1] ?? 'app'
          byPackage[owner] = (byPackage[owner] ?? '') + (rendered.code ?? '')
        }
        chunks[file] = byPackage
      }
      this.emitFile({
        type: 'asset',
        fileName: '.vite/modules.json',
        source: JSON.stringify(chunks),
      })
    },
  }
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), preloadMono(), moduleGraph()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    // scripts/budget.mjs reads the route closures from it (ADR 0009 P1 to P7).
    manifest: true,
    rollupOptions: {
      output: {
        // Every chunk a route needs is named in the entry's preload map, so a
        // name is paid for in the shell (ADR 0009 P1). Vue's virtual-module
        // suffix, `.vue_vue_type_script_setup_true_lang`, said nothing and cost
        // that much per chunk.
        chunkFileNames: (chunk) =>
          `assets/${chunk.name.replace(/\.vue_vue_type_.*$/u, '')}-[hash].js`,
        // The editor is a sixth of a megabyte and two pages use it. Left to
        // itself Rollup folds whatever those pages share with the rest — the
        // component barrel, the icons — into the editor's chunk, and the sign-in
        // page downloaded CodeMirror to draw a form.
        manualChunks(id) {
          // Fetched on demand, so kept out of the chunk every editor page loads.
          if (
            /node_modules\/(\.pnpm\/)?(@codemirror(\+|\/)(search|lang-sql)|@lezer(\+|\/)lr)/u.test(
              id,
            )
          ) {
            return undefined
          }
          return /node_modules\/(\.pnpm\/)?(@codemirror|@lezer|style-mod|w3c-keyname|crelt|@marijn)/u.test(
            id,
          )
            ? 'editor'
            : undefined
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    watch: {
      usePolling: true,
    },
    // The browser only ever talks to this origin, so no CORS surface exists and
    // the session cookie is first-party. AVIOR_PROXY_TARGET points at the
    // backend: the service name inside compose, localhost outside it.
    proxy: {
      '/api': {
        target: process.env['AVIOR_PROXY_TARGET'] ?? 'http://localhost:58080',
        changeOrigin: false,
      },
    },
  },
})
