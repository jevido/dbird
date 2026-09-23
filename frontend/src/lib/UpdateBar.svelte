<script lang="ts">
  import { Events } from '@wailsio/runtime';
  import { UpdateService } from '../../bindings/dbird';
  import type { UpdateStatus } from '../../bindings/dbird/models';
  import { app, errorText } from './state.svelte';

  let status = $state<UpdateStatus | null>(null);
  let restarting = $state(false);

  function load(el: HTMLElement) {
    UpdateService.Status().then((s) => (status = s)).catch(() => {});
    const off = Events.On('dbird:update', (e: { data: UpdateStatus }) => {
      const prev = status?.state;
      status = e.data;
      if (status.state === 'ready' && prev !== 'ready') {
        app.toast(`dbird ${status.latestVersion} is ready — restart to update`);
      }
    });
    return () => off();
  }

  async function check() {
    try {
      status = await UpdateService.CheckNow();
      if (status.state === 'up-to-date') app.toast(`dbird ${status.currentVersion} is up to date`);
      if (status.state === 'error') app.toast(`Update check failed: ${status.error}`, 'error');
    } catch (e) {
      app.toast(errorText(e), 'error');
    }
  }

  async function restart() {
    restarting = true;
    app.saveNow();
    try {
      await UpdateService.Restart();
    } catch (e) {
      restarting = false;
      app.toast(`Restart failed: ${errorText(e)}`, 'error');
    }
  }

  const busy = $derived(status?.state === 'checking' || status?.state === 'downloading');
</script>

<footer class="bar" {@attach load}>
  {#if status?.state === 'ready'}
    <button class="update ready" onclick={restart} disabled={restarting} title="Install the downloaded update and restart">
      {restarting ? 'Restarting…' : `Restart to update to ${status.latestVersion}`}
    </button>
  {:else if status?.state === 'manual' && status.packageManaged}
    <button class="update" onclick={() => UpdateService.OpenReleasePage()} title="Installed by your package manager; update it there (see the release page for the command)">
      {status.latestVersion} available — update with your package manager
    </button>
  {:else if status?.state === 'manual'}
    <button class="update" onclick={() => UpdateService.OpenReleasePage()} title="This install can't update itself; download the new version">
      {status.latestVersion} available — download
    </button>
  {:else}
    <span class="version" title={status?.checkedAt ? `Last checked ${new Date(status.checkedAt).toLocaleString()}` : ''}>
      dbird {status?.currentVersion ?? ''}
    </span>
    <span class="spacer"></span>
    {#if status && status.state !== 'disabled'}
      <button class="link" onclick={check} disabled={busy}>
        {status.state === 'checking' ? 'Checking…' : status.state === 'downloading' ? `Downloading ${status.latestVersion}…` : 'Check for updates'}
      </button>
    {/if}
  {/if}
</footer>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    padding: 0 10px;
    border-top: 1px solid var(--border);
    font-size: 11.5px;
    color: var(--text-faint);
    flex-shrink: 0;
  }
  .spacer {
    flex: 1;
  }
  .link {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 11.5px;
    cursor: pointer;
    padding: 0;
  }
  .link:hover:not(:disabled) {
    color: var(--accent);
  }
  .update {
    flex: 1;
    border: 1px solid var(--accent);
    background: var(--accent-bg);
    color: var(--text);
    border-radius: 5px;
    font-size: 11.5px;
    padding: 3px 8px;
    cursor: pointer;
  }
  .update.ready {
    background: var(--accent);
    color: #fff;
  }
</style>
