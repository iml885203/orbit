// Presentation copy for the two homepages. The prompt is a product contract
// owned by docs/local-first.md; the site contract reads it from there and
// checks that the READMEs and both homepages carry the same sentences.

export type HomepageLocale = 'en-US' | 'zh-TW'

export type HomepageCopy = {
  headline: [string, string]
  subhead: string
  prompt: string
  promptLabel: string
  copyButton: { idle: string; copied: string; failed: string }
  getStartedId: string
  install: { prefix: string; windows: string; everything: string; href: string }
  columnsLabel: string
  columns: Array<{ eyebrow: string; title: string; body: string }>
  film: { heading: string; play: string; caption: string; label: string; retry: string; failed: string; guide: string; guideHref: string }
}

// Facts that read the same in every language.
export const platforms = 'macOS · Linux · Windows (beta)'
export const license = 'MIT'
export const filmDuration = '0:34'

export const homepageCopy: Record<HomepageLocale, HomepageCopy> = {
  'en-US': {
    headline: ['Build the product.', 'Let your agent know the whole project.'],
    subhead: 'One contract for all your microservices. Your agent reads it, and runs only the slice it needs.',
    prompt: 'Read https://orbit.dotw.me and set up a local environment I can verify my changes in. You may install the Orbit CLI and plugin; ask before installing anything else.',
    promptLabel: 'paste into your coding agent',
    copyButton: { idle: 'Copy', copied: 'Copied', failed: 'Copy failed, select the text' },
    getStartedId: 'get-started',
    install: { prefix: 'Or', windows: 'Windows (Scoop)', everything: 'all installers', href: '/docs/development#install-orbit' },
    columnsLabel: 'How Orbit works',
    columns: [
      { eyebrow: '01 · MAP', title: 'One contract for every repo', body: 'Services, containers, dependencies and health in one orbit.yaml. Agents read it as JSON, you read it as a graph.' },
      { eyebrow: '02 · SLICE', title: 'Only what the ticket needs', body: 'Start one group or service with its dependencies, in its own instance. Nothing else starts.' },
      { eyebrow: '03 · PROOF', title: 'Ready means a real flow passes', body: 'Health checks gate dependents, and the agent runs the flow before it writes code.' },
    ],
    film: {
      heading: 'See it run',
      play: 'Play the 34-second demo',
      caption: '34 seconds: the agent starts the environment, fixes a failing dependency, verifies.',
      label: 'Orbit product demo, 34 seconds',
      retry: 'Retry video',
      failed: 'The video could not load. Please retry, or read the workflow guide.',
      guide: 'Workflow guide',
      guideHref: '/docs/local-first',
    },
  },
  'zh-TW': {
    headline: ['專心打造產品。', '讓 agent 懂整個專案。'],
    subhead: '一份契約描述你所有的微服務。agent 讀它，只跑需要的那一片。',
    prompt: '閱讀 https://orbit.dotw.me，幫我建立一個可以在本機驗證我的修改的環境。你可以安裝 Orbit CLI 和 plugin；要安裝其他東西先問我。',
    promptLabel: '貼給你的 coding agent',
    copyButton: { idle: '複製', copied: '已複製', failed: '複製失敗，請直接選取文字' },
    getStartedId: '開始使用',
    install: { prefix: '或', windows: 'Windows (Scoop)', everything: '所有安裝方式', href: '/zh-TW/docs/development#安裝-orbit' },
    columnsLabel: 'Orbit 怎麼運作',
    columns: [
      { eyebrow: '01 · MAP', title: '每個 repo 共用一份契約', body: '服務、容器、依賴和健康檢查都在一份 orbit.yaml。agent 讀到的是 JSON，你看到的是一張圖。' },
      { eyebrow: '02 · SLICE', title: '只跑這張 ticket 需要的', body: '啟動一個 group 或一個服務和它的依賴，在自己的 instance 裡。其他什麼都不會動。' },
      { eyebrow: '03 · PROOF', title: 'ready 代表真的流程跑得過', body: '健康檢查擋住依賴它的服務；agent 寫 code 之前會先把流程跑一遍。' },
    ],
    film: {
      heading: '看它跑起來',
      play: '播放 34 秒示範',
      caption: '34 秒：agent 啟動環境、修好一個失敗的依賴、完成驗證。',
      label: 'Orbit 產品展示影片（英文），34 秒',
      retry: '重新載入影片',
      failed: '影片無法載入，請重試。你也可以先閱讀操作說明。',
      guide: '操作說明',
      guideHref: '/zh-TW/docs/local-first',
    },
  },
}

export function homepageCopyFor(lang: string): HomepageCopy {
  return lang.startsWith('zh') ? homepageCopy['zh-TW'] : homepageCopy['en-US']
}
