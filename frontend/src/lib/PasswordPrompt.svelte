<script lang="ts">
  import { app, type PasswordRequest } from './state.svelte';

  let { req }: { req: PasswordRequest } = $props();

  let password = $state('');
  let show = $state(false);
  let remember = $state(true);

  const store = $derived(app.passwordStore);
  const target = $derived(
    `${req.conn.user ? `${req.conn.user}@` : ''}${req.conn.host || 'localhost'}${req.conn.database ? `/${req.conn.database}` : ''}`,
  );

  function submit(e: SubmitEvent) {
    e.preventDefault();
    req.resolve({ password, remember: remember && store.available });
  }

  function focus(el: HTMLInputElement) {
    el.focus();
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      req.resolve(null);
    }
  }}
/>

<div class="backdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && req.resolve(null)}>
  <form class="dialog" onsubmit={submit} aria-labelledby="pw-title">
    <h2 id="pw-title">Password for {req.conn.name}</h2>
    <p class="target">{target}</p>
    <div class="row">
      <input type={show ? 'text' : 'password'} bind:value={password} autocomplete="off" aria-label="Password" {@attach focus} />
      <button type="button" class="btn sm" onclick={() => (show = !show)}>{show ? 'Hide' : 'Show'}</button>
    </div>
    {#if store.available}
      <label class="check">
        <input type="checkbox" bind:checked={remember} />
        <span>Remember in {store.name}</span>
      </label>
    {:else}
      <p class="warn">
        No password store is available, so dbird can't save this password and will ask again next time. Set up a password store
        (GNOME Keyring, KWallet or KeePassXC) to have it remembered securely.
      </p>
    {/if}
    <footer>
      <button type="button" class="btn" onclick={() => req.resolve(null)}>Cancel</button>
      <button type="submit" class="btn primary">Connect</button>
    </footer>
  </form>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70;
    background: rgba(0, 0, 0, 0.5);
    display: grid;
    place-items: center;
    padding: 16px;
  }
  .dialog {
    width: min(420px, 100%);
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    padding: 16px 18px 12px;
  }
  h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
  }
  .target {
    margin: 4px 0 12px;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row {
    display: flex;
    gap: 6px;
  }
  .row input {
    flex: 1;
    min-width: 0;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 10px;
    color: var(--text-muted);
  }
  .warn {
    margin: 10px 0 0;
    font-size: 12px;
    line-height: 1.45;
    color: var(--syn-number);
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 14px;
  }
</style>
