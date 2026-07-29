<script lang="ts">
  import { onMount } from 'svelte'
  import Router from 'svelte-spa-router'
  import { subscribe, onConnectionLost } from '$lib/eventbus'
  import { startHistoryStream } from '$lib/history.svelte'
  import { onAppMount as extAppMount, onDaemonState as extDaemonState, onEnvChanged as extEnvChanged, onDaemonReset as extDaemonReset } from '$ext'
  import { store, replaceServices, replaceGraphData, appendLog, resetForNewDaemon } from '$lib/stores.svelte'
  import { fetchEnvToggles, fetchSettings, fetchVersion, fetchEnvs, fetchGraph } from '$lib/api'
  import { tracing, fetchTracingStatus, subscribeLiveTraces } from '$lib/tracing.svelte'
  import type { StatusResponse } from '$lib/types.gen'

  type LogMessage = { service: string; line: string }

  import Header from './components/Header.svelte'
  import SettingsPopover from './components/SettingsPopover.svelte'
  import EnvPopover from './components/EnvPopover.svelte'
  import Toast from './components/Toast.svelte'
  import VersionBanner from './components/VersionBanner.svelte'
  import HistoryBar from './components/HistoryBar.svelte'
  import { routes } from './routes'

  // The graph render only depends on these status fields (state, reason,
  // mode, ports, url, health progress) — NOT on the uptime strings that tick
  // every 2s. Refetching the graph only when this key changes turns the
  // steady-state 2s fetch+deep-compare loop into a no-op.
  function graphKeyOf(resp: StatusResponse): string {
    return (resp.resources ?? [])
      .map((s) => [
        s.name, s.state, s.state_reason ?? '', s.mode ?? '', s.url ?? '',
        s.logs_available ? 'logs' : '',
        JSON.stringify(s.ports ?? {}),
        s.health_progress ? `${s.health_progress.attempts}/${s.health_progress.last_err ?? ''}` : '',
      ].join('|'))
      .join(';')
  }
  let lastGraphKey = ''

  // DB-workflow availability (devMeta.db_configured) follows the active env —
  // a switch can add or remove the sql-server container — so refetch the dev
  // meta whenever the live graph lands on a different env.
  let lastMetaEnv = ''
  $effect(() => {
    const env = store.graph.data?.env
    if (!env || env === lastMetaEnv) return
    lastMetaEnv = env
    extEnvChanged()
  })

  function fetchDaemonState() {
    fetchEnvToggles().then(t => { store.daemon.envToggles = t })
    fetchSettings().then(s => {
      store.ui.showHistory = (s as typeof s & { show_history?: boolean }).show_history === true
      extDaemonState(s)
    })
    fetchEnvs().then(e => { store.daemon.envs = e })
    fetchGraph().then(g => { if (g) replaceGraphData(g) })
    fetchTracingStatus().then(s => { tracing.status = s })
  }

  onMount(() => {
    const cleanupStatus = subscribe('status', (data) => {
      const resp = data as StatusResponse
      if (store.daemon.daemonEpoch !== resp.epoch) {
        if (store.daemon.daemonEpoch !== null) {
          resetForNewDaemon()
          tracing.reset()
          extDaemonReset()
          fetchDaemonState()
        }
        store.daemon.daemonEpoch = resp.epoch
      }
      store.daemon.connected = true
      replaceServices(resp.resources || [])
      const key = graphKeyOf(resp)
      if (key !== lastGraphKey || !store.graph.data) {
        lastGraphKey = key
        fetchGraph().then(g => { if (g) replaceGraphData(g) })
      }
    })

    const cleanupConn = onConnectionLost(() => { store.daemon.connected = false })

    const cleanupLogs = subscribe('log', (data) => {
      const msg = data as LogMessage
      appendLog(msg.service, msg.line)
    })
    const cleanupHistory = startHistoryStream()
    const cleanupTraces = subscribeLiveTraces()
    const cleanupExt = extAppMount()

    fetchDaemonState()

    const refreshVersion = async () => {
      if (document.hidden) return
      const v = await fetchVersion()
      if (!v) return
      const prev = store.ui.version
      if (prev && prev.running === v.running && prev.update_available === v.update_available) return
      store.ui.version = v
    }
    refreshVersion()
    const versionInterval = setInterval(refreshVersion, 30_000)

    return () => {
      cleanupStatus()
      cleanupConn()
      cleanupLogs()
      cleanupHistory()
      cleanupTraces()
      cleanupExt()
      clearInterval(versionInterval)
    }
  })
</script>

<div class="app-shell" class:with-history={store.ui.showHistory}>
  <VersionBanner />
  <Header />
  <SettingsPopover />
  <EnvPopover />
  <main>
    <Router {routes} />
  </main>
  <Toast />
  <HistoryBar />
</div>

<style>
  .app-shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    min-height: 0;
    overflow: hidden;
  }

  .app-shell.with-history {
    padding-bottom: var(--history-bar-height);
  }

  main {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    overflow: auto;
  }
</style>
