import { defineConfig } from 'vitepress'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const siteOrigin = 'https://orbit.dotw.me'
const siteBase = '/'
const homepageTitle = 'Orbit — Local Dev Environments for Coding Agents'
const homepageDescription = 'Orbit gives coding agents one consistent way to start, inspect, and verify complete local development environments through a CLI and visual dashboard.'
const traditionalChineseHomepageDescription = 'Orbit 讓 coding agents 透過一致的 CLI 與視覺化 dashboard，啟動、檢查並驗證完整的本機開發環境。'
const untranslatedRoutes = new Set([
  'CODE_OF_CONDUCT',
  'CONTRIBUTING',
  'DESIGN',
  'SECURITY',
  'docs/1.0-test-matrix',
  'docs/testing',
  'docs/website',
])

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
  { text: '在你的專案使用 Orbit', link: '/zh-TW/docs/local-first' },
  { text: '為什麼選擇 Orbit', link: '/zh-TW/docs/why-orbit' },
  { text: '設定參考', link: '/zh-TW/docs/configuration' },
  { text: '環境來源', link: '/zh-TW/docs/environment-sources' },
  { text: '隔離的 runtime instances', link: '/zh-TW/docs/instances' },
  { text: 'E2E 測試', link: '/zh-TW/docs/e2e-testing' },
  { text: 'Tracing', link: '/zh-TW/docs/tracing' },
  { text: '疑難排解', link: '/zh-TW/docs/troubleshooting' },
  { text: '團隊導入', link: '/zh-TW/docs/team-adoption' },
]

const traditionalChineseOptional = [
  { text: 'SQL Server database projects', link: '/zh-TW/docs/sql-workflow' },
  { text: 'Tunnel claims', link: '/zh-TW/docs/tunnel-claim' },
]

const traditionalChineseReference = [
  { text: 'Agent CLI contract', link: '/zh-TW/docs/agent-cli' },
  { text: '架構', link: '/zh-TW/docs/architecture' },
  { text: '開發', link: '/zh-TW/docs/development' },
  { text: '版本與相容性', link: '/zh-TW/docs/versioning' },
  { text: '程式碼慣例', link: '/zh-TW/docs/CODE_CONVENTIONS' },
]

function localizedRoute(relativePath: string) {
  const match = relativePath.match(/^docs\/(.+)\.zh-TW\.md$/)
  return match ? `zh-TW/docs/${match[1]}.md` : relativePath
}

// The installers only deliver published GitHub Releases, so the homepage names
// the newest release tag rather than VERSION, which is the unreleased candidate
// between the prepare PR and the release. A checkout without tags yields
// nothing and the homepage omits the version segment.
function latestReleaseTag() {
  try {
    const tags = execFileSync('git', ['tag', '--list', 'v*', '--sort=-v:refname'], {
      cwd: fileURLToPath(new URL('../../', import.meta.url)),
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    return tags.split('\n').find((tag) => /^v\d+\.\d+\.\d+$/.test(tag))
  } catch (error) {
    console.warn('homepage release version omitted: git tag listing failed:', error)
    return undefined
  }
}

const latestRelease = latestReleaseTag()

// VitePress renders footer strings as HTML, which is the only way the default
// footer can carry links without a custom layout.
function footerLinks(links: Array<[string, string]>) {
  return links.map(([text, href]) => `<a href="${href}">${text}</a>`).join('')
}

function publicPath(relativePath: string) {
  const route = localizedRoute(relativePath).replace(/(?:index)?\.md$/, '')
  return `${siteBase}${route}`
}

function counterpartPath(relativePath: string) {
  const localized = localizedRoute(relativePath)
  if (localized === 'zh-TW/index.md') return siteBase
  if (localized.startsWith('zh-TW/docs/')) return `${siteBase}${localized.slice('zh-TW/'.length).replace(/\.md$/, '')}`
  if (localized === 'index.md') return `${siteBase}zh-TW/`
  if (localized.startsWith('docs/')) {
    const route = localized.replace(/\.md$/, '')
    return untranslatedRoutes.has(route) ? undefined : `${siteBase}zh-TW/${route}`
  }
  return undefined
}

export default defineConfig({
  srcDir: '..',
  title: 'Orbit',
  description: homepageDescription,
  lang: 'en-US',
  base: siteBase,
  appearance: 'dark',
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
      const renderLink = markdown.renderer.rules.link_open
      markdown.renderer.rules.link_open = (tokens, index, options, env, self) => {
        const hrefIndex = tokens[index].attrIndex('href')
        const href = hrefIndex >= 0 ? tokens[index].attrs?.[hrefIndex]?.[1] : undefined
        if (href === './README.md' || href === 'README.md') {
          tokens[index].attrs![hrefIndex][1] = '/'
        } else if (href === './README.zh-TW.md' || href === 'README.zh-TW.md') {
          tokens[index].attrs![hrefIndex][1] = '/zh-TW/'
        } else if (href?.startsWith('./docs/') && href.includes('.zh-TW')) {
          tokens[index].attrs![hrefIndex][1] = href.replace('./docs/', '/zh-TW/docs/').replace('.zh-TW', '')
        } else if (href?.includes('.zh-TW')) {
          tokens[index].attrs![hrefIndex][1] = href.replace(/^\.\//, '/zh-TW/docs/').replace('.zh-TW', '')
        } else if (localizedRoute(env.relativePath || '').startsWith('zh-TW/') && href && !/^(?:[a-z]+:|\/|#)/i.test(href)) {
          const englishTarget = href.replace(/^\.\//, '').replace(/\.md(?=#|$)/, '')
          tokens[index].attrs![hrefIndex][1] = englishTarget.startsWith('docs/')
            ? `/${englishTarget}`
            : env.relativePath?.includes('/docs/') || env.relativePath?.startsWith('docs/')
              ? `/docs/${englishTarget}`
              : `/${englishTarget}`
        }
        return renderLink ? renderLink(tokens, index, options, env, self) : self.renderToken(tokens, index, options)
      }
    },
  },
  sitemap: {
    hostname: 'https://orbit.dotw.me/',
  },
  srcExclude: [
    'launch-video/**',
    'AGENTS.md',
    'AGENTS.zh-TW.md',
    'CLAUDE.md',
    '.claude/**',
    '.agents/**',
    'plugins/**',
    'ui/**',
    'website/**',
    'README.md',
    'README.zh-TW.md',
    'docs/*.zh-TW.md',
  ],
  ignoreDeadLinks: [
    /^http:\/\/localhost/,
    /\.zh-TW(?:#|$)/,
    /^\.\/(?:CONTRIBUTING|testing|1\.0-test-matrix)$/,
  ],
  head: [
    ['meta', { name: 'theme-color', content: '#0d1117' }],
    ['link', { rel: 'icon', href: '/orbit-logo-badge.svg' }],
    ['link', { rel: 'ai-catalog', href: '/.well-known/ai-catalog.json', type: 'application/json' }],
  ],
  locales: {
    root: { label: 'English', lang: 'en-US' },
    'zh-TW': {
      label: '繁體中文',
      lang: 'zh-TW',
      link: '/zh-TW/',
      description: traditionalChineseHomepageDescription,
      themeConfig: {
        nav: [
          { text: '開始使用', link: '/zh-TW/docs/local-first' },
          { text: '給 Agent', link: 'https://orbit.dotw.me/agent/SKILL.md' },
          {
            text: '文件',
            items: [
              { text: '設定參考', link: '/zh-TW/docs/configuration' },
              { text: '疑難排解', link: '/zh-TW/docs/troubleshooting' },
              { text: '為什麼選擇 Orbit', link: '/zh-TW/docs/why-orbit' },
            ],
          },
        ],
        sidebar: { '/zh-TW/docs/': [
          { text: '指南', items: traditionalChineseGuide },
          { text: '選用工作流程', items: traditionalChineseOptional },
          { text: '參考與貢獻', items: traditionalChineseReference },
        ] },
        editLink: {
          pattern: ({ filePath }) => {
            const sourcePath = filePath.replace(/^zh-TW\/docs\/(.+)\.md$/, 'docs/$1.zh-TW.md')
            return `https://github.com/iml885203/orbit/edit/main/${sourcePath}`
          },
          text: '在 GitHub 編輯此頁',
        },
        footer: {
          message: '採用 MIT License 發布。',
          copyright: footerLinks([
            ['為什麼選擇 Orbit', '/zh-TW/docs/why-orbit'],
            ['文件', '/zh-TW/docs/local-first'],
            ['給 Agent', 'https://orbit.dotw.me/agent/SKILL.md'],
            ['GitHub', 'https://github.com/iml885203/orbit'],
            ['English', '/'],
          ]),
        },
      },
    },
  },
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
    if (pageData.relativePath.startsWith('zh-TW/')) pageData.lastUpdated = undefined
    if (pageData.relativePath === 'zh-TW/index.md') {
      pageData.title = 'Orbit（繁體中文）'
      pageData.frontmatter.description = traditionalChineseHomepageDescription
      pageData.frontmatter.latestRelease = latestRelease
      return
    }
    if (pageData.relativePath !== 'index.md') return

    pageData.title = 'Local Dev Environments for Coding Agents'
    pageData.frontmatter.description = homepageDescription
    pageData.frontmatter.latestRelease = latestRelease
  },
  transformHead({ pageData }) {
    const pagePath = publicPath(pageData.relativePath)
    const canonical = `${siteOrigin}${pagePath}`
    const alternate = counterpartPath(pageData.relativePath)
    const isTraditionalChinese = pageData.relativePath.startsWith('zh-TW/')
    const title = pagePath === siteBase
      ? homepageTitle
      : !pageData.title || pageData.title === 'Orbit' ? 'Orbit' : `${pageData.title} | Orbit`
    const summary = isTraditionalChinese
      ? traditionalChineseHomepageDescription
      : homepageDescription
    const description = pageData.frontmatter.description || (pageData.title ? `${pageData.title}. ${summary}` : summary)
    const locale = isTraditionalChinese ? 'zh_TW' : 'en_US'
    const language = isTraditionalChinese ? 'zh-TW' : 'en'
    const homepageSchema = pagePath === siteBase
      ? [[
          'script',
          { type: 'application/ld+json' },
          JSON.stringify({
            '@context': 'https://schema.org',
            '@type': 'SoftwareApplication',
            name: 'Orbit',
            description: summary,
            url: canonical,
            applicationCategory: 'DeveloperApplication',
            operatingSystem: 'macOS, Linux, Windows',
            codeRepository: 'https://github.com/iml885203/orbit',
            license: 'https://opensource.org/license/mit',
          }),
        ] as const]
      : []
    return [
      ['link', { rel: 'canonical', href: canonical }],
      ...(pageData.isNotFound ? [['meta', { name: 'robots', content: 'noindex, nofollow' }] as const] : []),
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:type', content: 'website' }],
      ['meta', { property: 'og:url', content: canonical }],
      ['meta', { property: 'og:locale', content: locale }],
      ['meta', { property: 'og:site_name', content: 'Orbit' }],
      ['meta', { property: 'og:image', content: `${siteOrigin}${siteBase}orbit-social-card.png` }],
      ['meta', { property: 'og:image:secure_url', content: `${siteOrigin}${siteBase}orbit-social-card.png` }],
      ['meta', { property: 'og:image:type', content: 'image/png' }],
      ['meta', { property: 'og:image:width', content: '1200' }],
      ['meta', { property: 'og:image:height', content: '630' }],
      ['meta', { property: 'og:image:alt', content: 'Orbit connects local development services for coding agents' }],
      ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
      ['meta', { name: 'twitter:image', content: `${siteOrigin}${siteBase}orbit-social-card.png` }],
      ['meta', { name: 'twitter:image:alt', content: 'Orbit connects local development services for coding agents' }],
      ['link', { rel: 'alternate', type: 'text/markdown', href: `${siteBase}agent/SKILL.md`, title: 'Orbit instructions for coding agents' }],
      ['link', { rel: 'alternate', hreflang: language, href: canonical }],
      ...(alternate ? [
        ['link', { rel: 'alternate', hreflang: locale === 'zh_TW' ? 'en' : 'zh-TW', href: `${siteOrigin}${alternate}` }],
      ] as const : []),
      ['link', { rel: 'alternate', hreflang: 'x-default', href: `${siteOrigin}${siteBase}` }],
      ...homepageSchema,
    ]
  },
  transformHtml(code) {
    for (const route of untranslatedRoutes) {
      code = code.replaceAll(`${siteBase}zh-TW/${route}`, `${siteBase}${route}`)
    }
    return code
  },
  themeConfig: {
    logo: '/orbit-logo-badge.svg',
    siteTitle: 'Orbit',
    nav: [
      { text: 'Get started', link: '/docs/local-first' },
      { text: 'For agents', link: 'https://orbit.dotw.me/agent/SKILL.md' },
      {
        text: 'Docs',
        items: [
          { text: 'Configuration', link: '/docs/configuration' },
          { text: 'Troubleshooting', link: '/docs/troubleshooting' },
          { text: 'Why Orbit', link: '/docs/why-orbit' },
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
      ],
    },
    search: {
      provider: 'local',
      options: {
        _render(src, env, markdown) {
          const contributorOnly = new Set([
            'docs/1.0-test-matrix.md',
            'docs/testing.md',
            'docs/website.md',
          ])
          return contributorOnly.has(env.relativePath) ? '' : markdown.render(src, env)
        },
        locales: {
          root: { translations: { button: { buttonText: 'Search', buttonAriaLabel: 'Search' } } },
          'zh-TW': { translations: { button: { buttonText: '搜尋', buttonAriaLabel: '搜尋' } } },
        },
      },
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
      copyright: footerLinks([
        ['Why Orbit', '/docs/why-orbit'],
        ['Docs', '/docs/local-first'],
        ['For agents', 'https://orbit.dotw.me/agent/SKILL.md'],
        ['GitHub', 'https://github.com/iml885203/orbit'],
        ['繁體中文', '/zh-TW/'],
      ]),
    },
  },
})
