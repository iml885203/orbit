import js from '@eslint/js'
import ts from 'typescript-eslint'
import svelte from 'eslint-plugin-svelte'
import svelteParser from 'svelte-eslint-parser'
import checkFile from 'eslint-plugin-check-file'
import globals from 'globals'

export default ts.config(
  {
    ignores: [
      'dist',
      'node_modules',
      // e2e artifacts — present locally after a playwright run, absent in CI.
      '.e2e-dist',
      'playwright-report',
      'test-results',
      'src/lib/types.gen.ts',
      // tygo output — generated code is exempt from style rules (e.g. empty
      // interfaces for Go structs with no exported fields).
      'src/lib/types/*.gen.ts',
    ],
  },
  js.configs.recommended,
  ...ts.configs.recommended,
  ...svelte.configs['flat/recommended'],
  {
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
  },
  {
    // Filename hygiene — see .claude/rules/domain-organization.md. Filenames
    // must name a domain, never a layer. utils/helpers/common/services are
    // magnets for unrelated code.
    files: ['src/**/*.{ts,js,svelte}'],
    plugins: { 'check-file': checkFile },
    rules: {
      'check-file/filename-blocklist': ['error', {
        '**/utils.{ts,js}': '*',
        '**/helpers.{ts,js}': '*',
        '**/common.{ts,js}': '*',
        '**/services.{ts,js}': '*',
        '**/misc.{ts,js}': '*',
      }],
    },
  },
  {
    files: ['**/*.svelte'],
    languageOptions: {
      parser: svelteParser,
      parserOptions: {
        parser: ts.parser,
        extraFileExtensions: ['.svelte'],
      },
    },
    rules: {
      // The trigger for this config: unkeyed {#each} let svelte recycle
      // DOM nodes by index across reactive updates, so SSE-driven
      // re-renders silently re-bound tooltips to the wrong row.
      'svelte/require-each-key': 'error',

      // a11y rules: eslint-plugin-svelte 3.x dropped its bundled a11y rules;
      // svelte-check covers a subset of these at type-check time. See
      // .claude/rules/svelte-a11y.md. Revisit when an eslint-plugin-svelte-a11y
      // replacement stabilises.
    },
  },
  {
    // *.svelte.ts / *.svelte.js — Svelte 5 rune modules (stores, actions).
    // flat/recommended already assigns svelte-eslint-parser to these files
    // but doesn't set a sub-parser, so TypeScript syntax fails to parse.
    files: ['**/*.svelte.ts', '**/*.svelte.js'],
    languageOptions: {
      parser: svelteParser,
      parserOptions: {
        parser: ts.parser,
      },
    },
  },
  {
    // Vitest globals.
    files: ['**/*.test.ts', 'src/test-setup.ts'],
    rules: {
      'no-undef': 'off',
      // Tests routinely have long setup blocks — exempt them.
      'max-lines': 'off',
      'max-lines-per-function': 'off',
    },
  },
  {
    rules: {
      // any has its place — types.gen + adapter shims. Don't fail on it.
      '@typescript-eslint/no-explicit-any': 'off',
      // Allow ts-expect-error etc; we're not strict.
      '@typescript-eslint/ban-ts-comment': 'off',
      // Unused vars: warn, allow _-prefix opt-out.
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],

      // Size / complexity budgets. Thresholds set to current ceiling — these
      // gate *regressions* not the existing baseline. Ratchet down with
      // refactors. Aspirational targets: max-lines 400, max-lines-per-function 50.
      'max-lines': ['warn', { max: 900, skipBlankLines: true, skipComments: true }],
      'max-lines-per-function': ['warn', { max: 200, skipBlankLines: true, skipComments: true, IIFEs: true }],
      'max-depth': ['warn', 5],

      // Debug log residue.
      'no-console': ['warn', { allow: ['warn', 'error'] }],
    },
  },
)
