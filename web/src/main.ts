import htmx from 'htmx.org';
import Alpine from 'alpinejs';

(window as any).htmx = htmx;
(window as any).Alpine = Alpine;
Alpine.start();

function initDelegatedCopyControls(): void {
  document.addEventListener('click', (event) => {
    const trigger = (event.target as HTMLElement | null)?.closest<HTMLElement>('[data-copy-target]');
    if (!trigger) return;
    const targetId = trigger.getAttribute('data-copy-target');
    if (!targetId) return;
    const target = document.getElementById(targetId);
    if (!target) return;
    const text = target.textContent || '';
    navigator.clipboard.writeText(text).then(() => {
      const orig = trigger.textContent || '';
      trigger.textContent = 'Copied!';
      setTimeout(() => { trigger.textContent = orig; }, 1500);
    }).catch(() => {});
  });
}

function initWorkbenchSwapFocus(): void {
  document.body.addEventListener('htmx:afterSwap', (event) => {
    const detail = (event as CustomEvent).detail;
    const target = detail?.target as HTMLElement | undefined;
    if (target?.id !== 'run-workbench-shell') return;
    const heading = target.querySelector<HTMLElement>('[data-run-step-heading]');
    heading?.focus({ preventScroll: true });
  });
}

initDelegatedCopyControls();
initWorkbenchSwapFocus();

function initDevReload(): void {
  const marker = document.querySelector('meta[name="relay-dev-reload"][content="enabled"]');
  if (!marker || typeof EventSource === 'undefined') return;

  let connectedOnce = false;
  let disconnectedAfterConnect = false;

  const connect = () => {
    const source = new EventSource('/dev/reload');

    source.onopen = () => {
      if (connectedOnce && disconnectedAfterConnect) {
        window.location.reload();
        return;
      }
      connectedOnce = true;
      disconnectedAfterConnect = false;
    };

    source.addEventListener('reload', () => {
      window.location.reload();
    });

    source.onerror = () => {
      if (connectedOnce) {
        disconnectedAfterConnect = true;
      }
      source.close();
      window.setTimeout(connect, 500);
    };
  };

  connect();
}

initDevReload();
