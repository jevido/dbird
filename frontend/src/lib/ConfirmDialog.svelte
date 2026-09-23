<script lang="ts">
  import type { ConfirmRequest } from './state.svelte';

  let { req }: { req: ConfirmRequest } = $props();

  // Focus Cancel, so Enter doesn't confirm by accident.
  function focusCancel(el: HTMLButtonElement) {
    el.focus();
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      req.resolve(false);
    }
  }}
/>

<div class="backdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && req.resolve(false)}>
  <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-msg">
    <header>
      <span class="icon" aria-hidden="true">!</span>
      <h2 id="confirm-title">{req.title}</h2>
    </header>
    <p id="confirm-msg">{req.message}</p>
    <div class="details">
      {#each req.details as d, i (i)}
        <pre>{d}</pre>
      {/each}
    </div>
    <footer>
      <button class="btn" onclick={() => req.resolve(false)} {@attach focusCancel}>Cancel</button>
      <button class="btn danger-solid" onclick={() => req.resolve(true)}>{req.confirmLabel}</button>
    </footer>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    background: rgba(0, 0, 0, 0.55);
    display: grid;
    place-items: center;
    padding: 16px;
  }
  .dialog {
    width: min(520px, 100%);
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-top: 3px solid var(--danger);
    border-radius: 10px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    padding: 16px 18px 14px;
  }
  header {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--danger);
    color: #fff;
    font-weight: 800;
    font-size: 13px;
    flex-shrink: 0;
  }
  h2 {
    margin: 0;
    font-size: 15px;
  }
  p {
    margin: 10px 0;
    line-height: 1.45;
  }
  .details {
    max-height: 180px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  pre {
    margin: 0;
    padding: 8px 10px;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-family: var(--mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 14px;
  }
  .danger-solid {
    background: var(--danger);
    border-color: var(--danger);
    color: #fff;
  }
  .danger-solid:hover {
    filter: brightness(1.08);
  }
</style>
