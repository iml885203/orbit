<script lang="ts">
  import { link, router } from 'svelte-spa-router'
  import { store } from '$lib/stores.svelte'
  import { navItems } from '../routes'
  import { tooltip } from '../lib/tooltip.svelte'
  import { ChevronDown, Eye, Settings } from '@lucide/svelte'

  const visibleNavItems = $derived(navItems.filter(item => !item.hidden?.()))
  const environmentContext = $derived(store.daemon.envs?.context)

  function isActive(path: string, loc: string): boolean {
    const l = loc || '/'
    if (path === '/') return l === '/'
    return l === path || l.startsWith(path + '/')
  }
</script>

<svelte:head>
  <title>{store.daemon.instanceName ? `${store.daemon.instanceName} · Orbit Dashboard` : 'Orbit Dashboard'}</title>
</svelte:head>

<header>
  <div class="brand">
    <img class="logo-mark" src="/orbit-logo.svg" alt="" aria-hidden="true">
    <h1>Orbit</h1>
  </div>
  {#if store.daemon.instanceName}
    <span class="instance-chip" aria-label={`Instance ${store.daemon.instanceName}`} title={store.daemon.instanceName}>
      <span class="instance-label">instance</span>
      <span class="instance-name">{store.daemon.instanceName}</span>
    </span>
  {/if}
  <span
    class="conn-dot"
    class:disconnected={!store.daemon.connected}
    use:tooltip={{ content: store.daemon.connected ? 'Connected' : 'Disconnected' }}
    role="status"
    aria-label={store.daemon.connected ? 'Connected' : 'Disconnected'}
  ></span>

  <nav aria-label="Primary navigation">
    {#each visibleNavItems as item (item.path)}
      <a
        href={'#' + item.path}
        use:link
        class="nav-link"
        class:active={isActive(item.path, router.location)}
      >{item.label}</a>
    {/each}
  </nav>

  {#if store.daemon.envs}
    <button
      class="env-chip"
      onclick={() => (store.ui.envPopoverOpen = !store.ui.envPopoverOpen)}
      title="Switch environment"
      aria-expanded={store.ui.envPopoverOpen}
      aria-haspopup="dialog"
    >
      <span class="env-label">{environmentContext?.kind === 'project' ? 'project' : 'env'}</span>
      <span class="env-name">{environmentContext?.display_name || store.daemon.envs.sources.flatMap(source => source.environments).find(env => env.selected)?.identity || '—'}</span>
      {#if environmentContext?.kind === 'project'}
        <span class="context-badge">Project environment</span>
      {:else if environmentContext?.kind === 'explicit'}
        <span class="context-badge">Explicit config</span>
      {/if}
      {#if store.graph.preview}
        <span class="preview"><Eye size={11} aria-hidden="true" /> {store.graph.preview.env}</span>
      {/if}
      <span class="chev" aria-hidden="true">
        <ChevronDown size={13} strokeWidth={2.25} />
      </span>
    </button>
  {/if}

  {#if store.ui.version?.running}
    <span
      class="version"
      class:stale={store.ui.version.update_available}
      use:tooltip={{ content: store.ui.version.running }}
    >{store.ui.version.running.split(' ')[0]}</span>
  {/if}

  <button
    class="settings-btn"
    onclick={() => store.ui.settingsOpen = !store.ui.settingsOpen}
    use:tooltip={{ content: 'Settings' }}
    aria-label="Settings"
  ><Settings size={18} strokeWidth={2} /></button>
</header>

<style>
  header {
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: 58px;
    background:
      linear-gradient(180deg, color-mix(in srgb, var(--card) 62%, transparent), transparent),
      var(--bg);
  }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 104px;
  }
  header h1 {
    font-size: var(--text-xl);
    font-weight: 700;
    letter-spacing: 0;
  }
  .logo-mark {
    width: 28px;
    height: 28px;
    display: block;
    flex-shrink: 0;
  }
  .instance-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 2px var(--space-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--card) 60%, transparent);
    color: var(--dim);
    font-size: var(--text-xs);
    white-space: nowrap;
    max-width: 200px;
    min-width: 0;
    flex-shrink: 1;
  }
  .instance-label {
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .instance-name {
    color: var(--fg);
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .conn-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--green);
    flex-shrink: 0;
    cursor: help;
    box-shadow: 0 0 10px color-mix(in srgb, var(--green) 55%, transparent);
  }
  .conn-dot.disconnected {
    background: var(--red);
    animation: pulse 1.5s infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
  nav {
    display: flex;
    align-items: center;
    gap: 0;
    margin-left: var(--space-3);
    padding: 3px;
    border: 1px solid color-mix(in srgb, var(--border) 86%, transparent);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--card) 54%, transparent);
  }
  .nav-link {
    color: var(--dim);
    text-decoration: none;
    font-size: var(--text-base);
    line-height: 1;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-md);
    transition: color 120ms, background 120ms, box-shadow 120ms;
  }
  .nav-link:hover {
    color: var(--fg);
    background: color-mix(in srgb, var(--fg) 5%, transparent);
  }
  .nav-link.active {
    color: var(--fg);
    background: color-mix(in srgb, var(--blue) 13%, var(--card));
    font-weight: 600;
    box-shadow:
      inset 0 0 0 1px color-mix(in srgb, var(--blue) 32%, transparent),
      0 0 14px color-mix(in srgb, var(--blue) 12%, transparent);
  }
  .env-chip {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    background: color-mix(in srgb, var(--card) 48%, transparent);
    border: 1px solid var(--border);
    color: var(--fg);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-md);
    font-size: var(--text-md);
    cursor: pointer;
    font-family: inherit;
    min-height: var(--hit-target);
  }
  .env-chip:hover {
    border-color: color-mix(in srgb, var(--blue) 42%, var(--dim));
    background: color-mix(in srgb, var(--blue) 7%, var(--card));
  }
  .env-label {
    color: var(--dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: var(--text-xs);
  }
  .env-name {
    font-family: var(--font-mono);
  }
  .preview {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    color: var(--blue);
    font-size: var(--text-xs);
  }
  .context-badge {
    color: var(--blue);
    background: color-mix(in srgb, var(--blue) 13%, transparent);
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-2);
    font-size: var(--text-xs);
  }
  .chev {
    color: var(--dim);
  }
  .version {
    display: inline-flex;
    align-items: center;
    min-height: var(--hit-target);
    font-family: var(--font-mono);
    font-size: var(--text-md);
    color: var(--dim);
    cursor: help;
  }
  .version.stale {
    color: var(--yellow);
  }
  .settings-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: color-mix(in srgb, var(--card) 38%, transparent);
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    color: var(--dim);
    cursor: pointer;
    padding: var(--space-1);
    min-width: var(--hit-target);
    min-height: var(--hit-target);
  }
  .settings-btn:hover {
    color: var(--fg);
    border-color: var(--border);
    background: color-mix(in srgb, var(--fg) 5%, transparent);
  }
</style>
