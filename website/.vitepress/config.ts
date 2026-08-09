import { defineConfig } from 'vitepress'
import { fileURLToPath } from 'node:url'

const englishGuide = [
  { text: 'Use Orbit with your project', link: '/docs/local-first' },
  { text: 'Why Orbit', link: '/docs/why-orbit' },
  { text: 'Configuration', link: '/docs/configuration' },
  { text: 'Environment sources', link: '/docs/environment-sources' },
  { text: 'Isolated runtime instances', link: '/docs/instances' },
  { text: 'E2E testing', link: '/docs/e2e-testing' },
  { text: 'Tracing', link: '/docs/tracing' },
  { text: 'Troubleshooting', link: '/docs/troubleshooting' },
]

const englishReference = [
  { text: 'Team adoption', link: '/docs/team-adoption' },
  { text: 'Agent CLI contract', link: '/docs/agent-cli' },
  { text: 'Architecture', link: '/docs/architecture' },
  { text: 'Development', link: '/docs/development' },
  { text: 'Testing strategy', link: '/docs/testing' },
  { text: 'Versioning', link: '/docs/versioning' },
  { text: 'Code conventions', link: '/docs/CODE_CONVENTIONS' },
  { text: 'Documentation website', link: '/docs/website' },
]

const traditionalChineseGuide = [
  { text: '在你的專案使用 Orbit', link: '/docs/local-first.zh-TW' },
  { text: '為什麼選擇 Orbit', link: '/docs/why-orbit.zh-TW' },
  { text: '設定參考', link: '/docs/configuration.zh-TW' },
  { text: '環境來源', link: '/docs/environment-sources.zh-TW' },
  { text: '隔離的 runtime instances', link: '/docs/instances.zh-TW' },
  { text: 'E2E 測試', link: '/docs/e2e-testing.zh-TW' },
  { text: 'Tracing', link: '/docs/tracing.zh-TW' },
  { text: '疑難排解', link: '/docs/troubleshooting.zh-TW' },
  { text: '團隊導入', link: '/docs/team-adoption.zh-TW' },
]

const traditionalChineseOptional = [
  { text: 'SQL Server database projects', link: '/docs/sql-workflow.zh-TW' },
  { text: 'Tunnel claims', link: '/docs/tunnel-claim.zh-TW' },
]

const traditionalChineseReference = [
  { text: 'Agent CLI contract', link: '/docs/agent-cli.zh-TW' },
  { text: '架構', link: '/docs/architecture.zh-TW' },
  { text: '開發', link: '/docs/development.zh-TW' },
  { text: '版本與相容性', link: '/docs/versioning.zh-TW' },
]

export default defineConfig({
  srcDir: '..',
  title: 'Orbit',
  description: 'One observable local environment for every service your project needs.',
  lang: 'en-US',
  base: '/orbit/',
  cleanUrls: true,
  lastUpdated: true,
  markdown: {
    html: false,
    config(markdown) {
      const renderCode = markdown.renderer.rules.code_inline
      markdown.renderer.rules.code_inline = (tokens, index, options, env, self) => {
        const rendered = renderCode
          ? renderCode(tokens, index, options, env, self)
          : self.renderToken(tokens, index, options)
        return rendered.replaceAll('{', '&#123;').replaceAll('}', '&#125;')
      }
    },
  },
  sitemap: {
    hostname: 'https://iml885203.github.io/orbit/',
  },
  rewrites: {
    'README.md': 'index.md',
  },
  srcExclude: [
    'AGENTS.md',
    'AGENTS.zh-TW.md',
    'CLAUDE.md',
    '.claude/**',
    '.agents/**',
    'plugins/**',
    'ui/**',
    'website/**',
  ],
  ignoreDeadLinks: [/^http:\/\/localhost/],
  head: [
    ['meta', { name: 'theme-color', content: '#0d1117' }],
    ['link', { rel: 'icon', href: '/orbit/orbit-logo-badge.svg' }],
  ],
  vite: {
    publicDir: fileURLToPath(new URL('../../ui/public', import.meta.url)),
    resolve: {
      // Markdown modules live above website/, so Vite cannot discover the
      // package-local Vue entry points by walking up from each source file.
      alias: [
        { find: 'vue/server-renderer', replacement: fileURLToPath(import.meta.resolve('vue/server-renderer')) },
        {
          find: /^vue$/,
          replacement: fileURLToPath(new URL('./dist/vue.runtime.esm-bundler.js', import.meta.resolve('vue/package.json'))),
        },
      ],
    },
  },
  transformPageData(pageData) {
    if (pageData.relativePath === 'README.zh-TW.md') {
      pageData.title = 'Orbit（繁體中文）'
      return
    }
    if (pageData.relativePath !== 'index.md') return

    pageData.title = 'Orbit'
    pageData.frontmatter.layout = 'home'
    pageData.frontmatter.hero = {
      name: 'Orbit',
      text: 'Your whole local stack, one observable environment.',
      tagline: 'Start host processes and containers in dependency order. See readiness, logs, ports, traces, and failures from one CLI and dashboard.',
      image: {
        src: '/orbit-logo-badge.svg',
        alt: 'Orbit',
      },
      actions: [
        { theme: 'brand', text: 'Get started', link: '/docs/local-first' },
        { theme: 'alt', text: 'Install Orbit', link: '/docs/development' },
      ],
    }
    pageData.frontmatter.features = [
      { title: 'Start together', details: 'Bring up containers and host processes in dependency order from one versioned file.' },
      { title: 'Know what is ready', details: 'Inspect health, logs, ports, traces, and failures from the dashboard or stable JSON CLI.' },
      { title: 'Run the same stack', details: 'Share one environment definition across developers, CI jobs, and coding agents.' },
    ]
  },
  themeConfig: {
    logo: '/orbit-logo-badge.svg',
    siteTitle: 'Orbit',
    nav: [
      { text: 'Get started', link: '/docs/local-first' },
      { text: 'Configuration', link: '/docs/configuration' },
      { text: 'Troubleshooting', link: '/docs/troubleshooting' },
      {
        text: '繁體中文',
        items: [
          { text: '開始使用', link: '/docs/local-first.zh-TW' },
          { text: '設定參考', link: '/docs/configuration.zh-TW' },
          { text: '疑難排解', link: '/docs/troubleshooting.zh-TW' },
          { text: '架構', link: '/docs/architecture.zh-TW' },
          { text: 'Agent CLI', link: '/docs/agent-cli.zh-TW' },
        ],
      },
    ],
    sidebar: {
      '/docs/': [
        { text: 'Guides', items: englishGuide },
        {
          text: 'Optional workflows',
          items: [
            { text: 'SQL Server database projects', link: '/docs/sql-workflow' },
            { text: 'Tunnel claims', link: '/docs/tunnel-claim' },
          ],
        },
        { text: 'Reference and contribution', items: englishReference },
        {
          text: '繁體中文',
          collapsed: true,
          items: [
            { text: '指南', items: traditionalChineseGuide },
            { text: '選用工作流程', items: traditionalChineseOptional },
            { text: '參考與貢獻', items: traditionalChineseReference },
          ],
        },
      ],
    },
    search: {
      provider: 'local',
    },
    editLink: {
      pattern: 'https://github.com/iml885203/orbit/edit/main/:path',
      text: 'Edit this page on GitHub',
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/iml885203/orbit' },
    ],
    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Orbit is built in the open.',
    },
  },
})
