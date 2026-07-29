import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { svelteTesting } from '@testing-library/svelte/vite'
import path from 'path'

// Filter out the hot-update plugin: it uses server.environments (Vite 8 API)
// which doesn't exist in vitest's embedded Vite 5 server.
const sveltePlugins = ([] as ReturnType<typeof svelte>).concat(svelte()).filter(
  (p) => !('name' in p && p.name === 'vite-plugin-svelte:hot-update')
)

export default defineConfig({
  plugins: [...sveltePlugins, svelteTesting()],
  resolve: {
    // Mirrors vite.config.ts's alias order and overlay seams.
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
  server: {
    fs: {
      // Allow serving the overlay's out-of-tree extension sources in
      // test runs (vitest's embedded server blocks /@fs/ paths outside
      // the root by default).
      allow: ['.', ...(process.env.ORBIT_UI_EXT ? [path.resolve(process.env.ORBIT_UI_EXT)] : [])],
    },
  },
  test: {
    environment: 'jsdom',
    // VM threads isolate each test file without recompiling the Svelte graph
    // in separate processes. This keeps the full UI suite below ten seconds
    // on a development Mac while preserving per-file globals.
    pool: 'vmThreads',
    clearMocks: true,
    // An overlay run's extension tests live outside this tree — include
    // them alongside the core's when ORBIT_UI_EXT points there.
    include: [
      'src/**/*.test.ts',
      ...(process.env.ORBIT_UI_EXT ? [path.resolve(process.env.ORBIT_UI_EXT, '**/*.test.ts').replaceAll('\\', '/')] : []),
    ],
    setupFiles: ['./src/test-setup.ts'],
  },
})
