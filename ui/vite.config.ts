import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import path from 'path'

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    // Order matters: the exact $lib/types.gen entry must precede the $lib
    // prefix. ORBIT_UI_TYPES lets an overlay build swap the type barrel
    // for one that re-exports the core barrel plus its own generated
    // types (the two-module tygo split); ORBIT_UI_EXT swaps the
    // extension module; $components exists so out-of-tree extension
    // sources can reach shared components without relative paths into
    // the submodule.
    alias: [
      { find: '$lib/types.gen', replacement: path.resolve(process.env.ORBIT_UI_TYPES ?? './src/lib/types.gen.ts') },
      // The .ts-suffixed spelling must hit the same override — otherwise it
      // would silently fall through to the $lib prefix and bypass the
      // overlay barrel.
      { find: '$lib/types.gen.ts', replacement: path.resolve(process.env.ORBIT_UI_TYPES ?? './src/lib/types.gen.ts') },
      { find: '$lib', replacement: path.resolve('./src/lib') },
      { find: '$components', replacement: path.resolve('./src/components') },
      // Exact-match to the module entry file: rolldown does not apply
      // directory-index resolution to alias targets outside the vite root.
      { find: /^\$ext$/, replacement: path.resolve(process.env.ORBIT_UI_EXT ?? './src/ext', 'index.ts') },
      { find: '$ext', replacement: path.resolve(process.env.ORBIT_UI_EXT ?? './src/ext') },
    ],
  },
  build: {
    // An overlay build (where this repo is a submodule) points
    // ORBIT_UI_OUTDIR at its own binary's dist; the default targets the
    // neutral binary next to this tree.
    outDir: process.env.ORBIT_UI_OUTDIR ?? '../cmd/orbit/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 600,
    rolldownOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes('node_modules/three/')) return 'vendor-three'
          if (id.includes('node_modules/@xyflow/') || id.includes('node_modules/@dagrejs/')) return 'vendor-graph'
          if (id.includes('node_modules/gsap/')) return 'vendor-gsap'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:19800',
      },
    },
  },
})
