import { defineConfig } from 'vitepress'
import { fileURLToPath } from 'node:url'

const siteOrigin = 'https://orbit.dotw.me'
const siteBase = '/'
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
  if (relativePath === 'README.zh-TW.md') return 'zh-TW/index.md'
  const match = relativePath.match(/^docs\/(.+)\.zh-TW\.md$/)
  return match ? `zh-TW/docs/${match[1]}.md` : relativePath
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
  description: "Let coding agents run and verify your project's local environment.",
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
      const renderImage = markdown.renderer.rules.image
      markdown.renderer.rules.image = (tokens, index, options, env, self) => {
        const srcIndex = tokens[index].attrIndex('src')
        const src = srcIndex >= 0 ? tokens[index].attrs?.[srcIndex]?.[1] : undefined
        if (env.relativePath === 'zh-TW/index.md' && src === 'ui/public/orbit-logo-badge.svg') {
          tokens[index].attrs![srcIndex][1] = '/orbit-logo-badge.svg'
        }
        if (env.relativePath === 'zh-TW/index.md' && src === 'docs/assets/orbit-demo-dashboard.jpg') {
          tokens[index].attrs![srcIndex][1] = 'https://raw.githubusercontent.com/iml885203/orbit/main/docs/assets/orbit-demo-dashboard.jpg'
        }
        return renderImage ? renderImage(tokens, index, options, env, self) : self.renderToken(tokens, index, options)
      }
    },
  },
  sitemap: {
    hostname: 'https://orbit.dotw.me/',
  },
  rewrites: { 'README.md': 'index.md' },
  srcExclude: [
    'AGENTS.md',
    'AGENTS.zh-TW.md',
    'CLAUDE.md',
    '.claude/**',
    '.agents/**',
    'plugins/**',
    'ui/**',
    'website/**',
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
  ],
  locales: {
    root: { label: 'English', lang: 'en-US' },
    'zh-TW': {
      label: '繁體中文',
      lang: 'zh-TW',
      link: '/zh-TW/',
      description: '讓 coding agents 跑起並驗證專案的本機環境。',
      themeConfig: {
        nav: [
          { text: '開始使用', link: '/zh-TW/docs/local-first' },
          { text: '設定參考', link: '/zh-TW/docs/configuration' },
          { text: '疑難排解', link: '/zh-TW/docs/troubleshooting' },
          { text: '為什麼選擇 Orbit', link: '/zh-TW/docs/why-orbit' },
        ],
        sidebar: { '/zh-TW/docs/': [
          { text: '指南', items: traditionalChineseGuide },
          { text: '選用工作流程', items: traditionalChineseOptional },
          { text: '參考與貢獻', items: traditionalChineseReference },
        ] },
        editLink: {
          pattern: ({ filePath }) => {
            const sourcePath = filePath === 'zh-TW/index.md'
              ? 'README.zh-TW.md'
              : filePath.replace(/^zh-TW\/docs\/(.+)\.md$/, 'docs/$1.zh-TW.md')
            return `https://github.com/iml885203/orbit/edit/main/${sourcePath}`
          },
          text: '在 GitHub 編輯此頁',
        },
        footer: { message: '採用 MIT License 發布。', copyright: 'Orbit 以開放方式打造。' },
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
      pageData.frontmatter.layout = 'home'
      pageData.frontmatter.hero = {
        name: 'Orbit',
        text: '專心打造產品，讓 Agent 把專案跑起來。',
        tagline: '把一個網址交給 coding agent。Orbit 會協助它取得工具、啟動專案需要的本機環境，並驗證應用程式真的可以使用。',
        image: { src: '/orbit-logo-badge.svg', alt: 'Orbit' },
        actions: [
          { theme: 'brand', text: '與 Agent 開始使用', link: '/zh-TW/#開始使用' },
        ],
      }
      pageData.frontmatter.features = [
        { title: '只要一個需求', details: '把 Orbit 網址交給 coding agent；它會取得工具並理解現有專案。' },
        { title: '留下可重現的設定', details: '成功的方法會保存在程式碼旁的 orbit.yaml，不再只存在某個人的腦中。' },
        { title: '證明應用真的可用', details: 'Orbit 回報預期環境的狀態，Agent 再驗證適合該 application 的代表性行為。' },
      ]
      return
    }
    if (pageData.relativePath !== 'index.md') return

    pageData.title = 'Orbit'
    pageData.frontmatter.layout = 'home'
    pageData.frontmatter.hero = {
      name: 'Orbit',
      text: 'Build the product. Let your agent run the project.',
      tagline: 'Give your coding agent one URL. Orbit helps it get the tools, start the local environment this project needs, and verify that the application actually works.',
      image: {
        src: '/orbit-logo-badge.svg',
        alt: 'Orbit',
      },
      actions: [
        { theme: 'brand', text: 'Get started with your agent', link: '/#get-started' },
      ],
    }
    pageData.frontmatter.features = [
      { title: 'One request to your agent', details: 'Give it the Orbit URL; it gets the tools and understands the existing project.' },
      { title: 'One repeatable setup', details: 'The working environment stays beside the code in orbit.yaml instead of one person’s head.' },
      { title: 'Proof that the app works', details: 'Orbit reports the intended environment; the agent verifies representative application behavior.' },
    ]
  },
  transformHead({ pageData }) {
    const pagePath = publicPath(pageData.relativePath)
    const canonical = `${siteOrigin}${pagePath}`
    const alternate = counterpartPath(pageData.relativePath)
    const isTraditionalChinese = pageData.relativePath.startsWith('zh-TW/')
    const title = !pageData.title || pageData.title === 'Orbit' ? 'Orbit' : `${pageData.title} | Orbit`
    const summary = isTraditionalChinese
      ? '讓 coding agents 跑起並驗證專案的本機環境。'
      : "Let coding agents run and verify your project's local environment."
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
      ['meta', { property: 'og:image', content: 'https://raw.githubusercontent.com/iml885203/orbit/main/docs/assets/orbit-demo-dashboard.jpg' }],
      ['meta', { property: 'og:image:alt', content: 'Orbit local development dashboard' }],
      ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
      ['meta', { name: 'twitter:image', content: 'https://raw.githubusercontent.com/iml885203/orbit/main/docs/assets/orbit-demo-dashboard.jpg' }],
      ['meta', { name: 'twitter:image:alt', content: 'Orbit local development dashboard' }],
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
      { text: 'Configuration', link: '/docs/configuration' },
      { text: 'Troubleshooting', link: '/docs/troubleshooting' },
      { text: 'Why Orbit', link: '/docs/why-orbit' },
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
      copyright: 'Orbit is built in the open.',
    },
  },
})
