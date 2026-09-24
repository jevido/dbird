<script lang="ts">
  import { Browser } from '@wailsio/runtime';
  import { ChangelogService } from '../../bindings/dbird';
  import type { Entry } from '../../bindings/dbird/internal/changelog/models';
  import logo from '../assets/logo.png';

  let { entries, onclose }: { entries: Entry[]; onclose: () => void } = $props();

  const MAX_ITEMS = 12; // keep it short; the rest is on GitHub

  const latest = $derived(entries[0]);
  // Trim to MAX_ITEMS across releases, newest first.
  const shown = $derived.by(() => {
    let left = MAX_ITEMS;
    return entries
      .map((e) => {
        const items = (e.items ?? []).slice(0, Math.max(0, left));
        left -= items.length;
        return { ...e, items };
      })
      .filter((e) => e.items.length > 0);
  });
  const hidden = $derived(entries.reduce((n, e) => n + (e.items?.length ?? 0), 0) - shown.reduce((n, e) => n + e.items.length, 0));

  function date(d: string) {
    const t = new Date(d + 'T00:00:00');
    return isNaN(t.getTime()) ? d : t.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }

  function close() {
    ChangelogService.Seen().catch(() => {});
    onclose();
  }

  async function openNotes() {
    Browser.OpenURL(await ChangelogService.ReleaseURL()).catch(() => {});
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && close()} />

<div class="backdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && close()}>
  <div class="card" role="dialog" aria-modal="true" aria-labelledby="wn-title">
    <header>
      <img src={logo} alt="" class="logo" />
      <div>
        <h2 id="wn-title">What's new in dbird</h2>
        <p class="sub">You're now on <span class="pill">{latest.version}</span></p>
      </div>
    </header>

    <div class="body">
      {#each shown as e (e.version)}
        <section>
          {#if entries.length > 1}
            <h3>{e.version} <span>{date(e.date)}</span></h3>
          {/if}
          <ul>
            {#each e.items as item, i (i)}
              <li>{item}</li>
            {/each}
          </ul>
        </section>
      {/each}
      {#if hidden > 0}
        <p class="more">…and {hidden} more {hidden === 1 ? 'change' : 'changes'}.</p>
      {/if}
    </div>

    <footer>
      <button class="link" onclick={openNotes}>Release notes on GitHub</button>
      <button class="btn primary" onclick={close} {@attach (el) => el.focus()}>Got it</button>
    </footer>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 90;
    display: grid;
    place-items: center;
    padding: 16px;
    background: rgba(0, 0, 0, 0.45);
    backdrop-filter: blur(3px);
    animation: fade 0.18s ease-out;
  }
  .card {
    width: min(480px, 100%);
    max-height: calc(100vh - 32px);
    display: flex;
    flex-direction: column;
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 14px;
    box-shadow: 0 24px 70px rgba(0, 0, 0, 0.45);
    overflow: hidden;
    animation: rise 0.22s cubic-bezier(0.2, 0.9, 0.3, 1.2);
  }
  header {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 20px 22px 14px;
    background: linear-gradient(to bottom, var(--accent-bg), transparent);
  }
  .logo {
    width: 44px;
    height: 44px;
    flex-shrink: 0;
  }
  h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 650;
  }
  .sub {
    margin: 3px 0 0;
    color: var(--text-muted);
    font-size: 12.5px;
  }
  .pill {
    display: inline-block;
    padding: 0 7px;
    border-radius: 999px;
    background: var(--accent);
    color: var(--bg);
    font-weight: 600;
    font-size: 11.5px;
    font-family: var(--mono);
  }
  .body {
    padding: 4px 22px 6px;
    overflow: auto;
  }
  section + section {
    margin-top: 12px;
  }
  h3 {
    margin: 0 0 6px;
    font-size: 12px;
    font-family: var(--mono);
    color: var(--text);
  }
  h3 span {
    color: var(--text-faint);
    font-family: var(--sans);
    font-weight: 400;
    margin-left: 6px;
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    position: relative;
    padding: 5px 0 5px 18px;
    line-height: 1.4;
    font-size: 13px;
  }
  li::before {
    content: '';
    position: absolute;
    left: 3px;
    top: 11px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
  }
  .more {
    margin: 6px 0 0;
    color: var(--text-muted);
    font-size: 12.5px;
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 12px 22px 16px;
    border-top: 1px solid var(--border);
    margin-top: 10px;
  }
  .link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 12.5px;
    cursor: pointer;
  }
  .link:hover {
    text-decoration: underline;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.98);
    }
  }
</style>
