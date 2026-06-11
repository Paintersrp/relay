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
    const heading = target.querySelector<HTMLElement>('[data-run-stage-heading]');
    heading?.focus({ preventScroll: true });
  });
}

function initWorkbenchBusyIndicator(): void {
  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const detail = (event as CustomEvent).detail;
    const target = detail?.target as HTMLElement | undefined;
    if (target?.id !== 'run-workbench-shell') return;
    target.setAttribute('aria-busy', 'true');
  });
  document.body.addEventListener('htmx:afterSettle', (event) => {
    const detail = (event as CustomEvent).detail;
    const target = detail?.target as HTMLElement | undefined;
    if (target?.id !== 'run-workbench-shell') return;
    target.removeAttribute('aria-busy');
  });
}

const RELAY_ACTION_FORM_SELECTOR = 'form[data-relay-workbench-action="true"], form[data-relay-settings-action="true"]';

function initRelayActionSubmitState(): void {
  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const detail = (event as CustomEvent).detail;
    const elt = detail?.elt as HTMLElement | undefined;
    if (!elt) return;
    const form = elt instanceof HTMLFormElement && elt.matches(RELAY_ACTION_FORM_SELECTOR)
      ? elt
      : elt.closest(RELAY_ACTION_FORM_SELECTOR);
    if (!form) return;
    form.setAttribute('data-relay-submitting', 'true');
    form.setAttribute('aria-busy', 'true');
    const controls = form.querySelectorAll<HTMLButtonElement | HTMLInputElement>(
      'button[type="submit"], button:not([type]), input[type="submit"], input[type="button"]'
    );
    controls.forEach((ctrl) => {
      if (!ctrl.disabled) {
        ctrl.setAttribute('data-relay-submit-was-enabled', 'true');
        ctrl.disabled = true;
      }
    });
  });
  document.body.addEventListener('htmx:afterRequest', (event) => {
    const detail = (event as CustomEvent).detail;
    const elt = detail?.elt as HTMLElement | undefined;
    if (!elt) return;
    const form = elt instanceof HTMLFormElement && elt.matches(RELAY_ACTION_FORM_SELECTOR)
      ? elt
      : elt.closest(RELAY_ACTION_FORM_SELECTOR);
    if (!form) return;
    form.removeAttribute('data-relay-submitting');
    form.removeAttribute('aria-busy');
    const controls = form.querySelectorAll<HTMLElement>(
      '[data-relay-submit-was-enabled="true"]'
    );
    controls.forEach((ctrl) => {
      if (ctrl instanceof HTMLButtonElement || ctrl instanceof HTMLInputElement) {
        ctrl.disabled = false;
      }
      ctrl.removeAttribute('data-relay-submit-was-enabled');
    });
  });
}

function initArtifactPreviewControls(): void {
  document.addEventListener('click', (event) => {
    const trigger = (event.target as HTMLElement | null)?.closest<HTMLElement>('[data-relay-clear-artifact-preview="true"]');
    if (!trigger) return;
    const preview = document.getElementById('run-artifact-preview');
    if (!preview) return;
    const empty = document.createElement('section');
    empty.id = 'run-artifact-preview';
    empty.className = 'relay-artifact-preview-slot';
    empty.setAttribute('aria-live', 'polite');
    preview.replaceWith(empty);
  });
}

initDelegatedCopyControls();
initWorkbenchSwapFocus();
initWorkbenchBusyIndicator();
initRelayActionSubmitState();
initArtifactPreviewControls();

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
