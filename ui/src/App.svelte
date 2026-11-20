<script lang="ts">
  import { onMount } from 'svelte'
  import Router from 'svelte-spa-router'
  import { subscribe, onConnectionLost } from '$lib/eventbus'
  import { startHistoryStream } from '$lib/history.svelte'
  import { onAppMount as extAppMount, onDaemonState as extDaemonState, onEnvChanged as extEnvChanged, onDaemonReset as extDaemonReset } from '$ext'
  import { store, replaceServices, appendLog, resetForNewDaemon } from '$lib/stores.svelte'
  import { fetchEnvToggles, fetchSettings, fetchVersion, fetchEnvs } from '$lib/api'
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

  let lastMetaContext = ''

  function fetchDaemonState() {
    fetchEnvToggles().then(t => { store.daemon.envToggles = t })
    fetchSettings().then(s => {
      store.ui.showHistory = (s as typeof s & { show_history?: boolean }).show_history === true
		store.ui.automaticUpdates = (s as typeof s & { automatic_updates?: 'automatic' | 'off' }).automatic_updates ?? 'automatic'
      extDaemonState(s)
    })
    fetchEnvs().then(e => { store.daemon.envs = e })
    store.graph.refresh()
    fetchTracingStatus().then(s => { tracing.status = s })
  }

  onMount(() => {
    const cleanupStatus = subscribe('status', (data) => {
      const resp = data as StatusResponse
      const metaContext = JSON.stringify([resp.epoch, resp.generation, resp.config_path, resp.context?.identity])
      const daemonRestarted = store.daemon.daemonEpoch !== null && store.daemon.daemonEpoch !== resp.epoch
      if (store.daemon.daemonEpoch !== resp.epoch) {
        if (store.daemon.daemonEpoch !== null) {
          resetForNewDaemon()
          tracing.reset()
          extDaemonReset()
          fetchDaemonState()
        }
        store.daemon.daemonEpoch = resp.epoch
      }
      if (metaContext !== lastMetaContext) {
        lastMetaContext = metaContext
        if (!daemonRestarted) extEnvChanged()
      }
      store.daemon.instanceName = resp.instance ?? ''
      store.daemon.connected = true
      store.daemon.environmentIdentity = resp.context?.identity ?? ''
      replaceServices(resp.resources || [], { epoch: resp.epoch, generation: resp.generation, config_path: resp.config_path })
      store.graph.observeStatus(resp)
    })

    const cleanupConn = onConnectionLost(() => { store.daemon.connected = false; store.graph.disconnect() })

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
      if (prev &&
        prev.running === v.running &&
        prev.on_disk === v.on_disk &&
        prev.on_disk_path === v.on_disk_path &&
        prev.update_available === v.update_available) return
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
      store.graph.dispose()
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
