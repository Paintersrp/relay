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
    initRunEventStream();
  });
}

function initWorkbenchBusyIndicator(): void {
  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const detail = (event as CustomEvent).detail;
    const target = detail?.target as HTMLElement | undefined;
    if (target?.id !== 'run-workbench-shell') return;
    target.setAttribute('aria-busy', 'true');
    relayRunRefreshInFlight = true;
  });
  document.body.addEventListener('htmx:afterSettle', (event) => {
    const detail = (event as CustomEvent).detail;
    const target = detail?.target as HTMLElement | undefined;
    if (target?.id !== 'run-workbench-shell') return;
    target.removeAttribute('aria-busy');
    relayRunRefreshInFlight = false;
    flushWorkbenchRefreshQueue();
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

function initHTMXErrorBanner(): void {
  const host = document.querySelector<HTMLElement>('[data-relay-htmx-error]');
  const message = document.querySelector<HTMLElement>('[data-relay-htmx-error-message]');
  if (!host || !message) return;

  const show = (text: string): void => {
    message.textContent = text;
    host.classList.remove('hidden');
    host.setAttribute('data-relay-visible', 'true');
  };

  const hide = (): void => {
    message.textContent = '';
    host.classList.add('hidden');
    host.removeAttribute('data-relay-visible');
  };

  document.addEventListener('click', (event) => {
    const trigger = (event.target as HTMLElement | null)?.closest('[data-relay-dismiss-htmx-error="true"]');
    if (!trigger) return;
    hide();
  });

  document.body.addEventListener('htmx:responseError', (event) => {
    const detail = (event as CustomEvent).detail;
    const xhr = detail?.xhr as XMLHttpRequest | undefined;
    const status = xhr?.status;
    const statusText = xhr?.statusText || 'request failed';

    let text = 'Relay could not update this section. Try again or open the full page.';
    if (status === 404) {
      text = 'Relay could not find the requested content.';
    } else if (status === 400) {
      const responseText = (xhr?.responseText || '').trim();
      text = responseText || 'Relay rejected the request.';
    } else if (status && status >= 500) {
      text = 'Relay hit a server error while updating this section.';
    } else if (status) {
      text = `Relay request failed (${status} ${statusText}).`;
    }

    show(text);
  });

  document.body.addEventListener('htmx:sendError', () => {
    show('Relay could not reach the local server. Check that the app is still running.');
  });

  document.body.addEventListener('htmx:afterSwap', (event) => {
    const detail = (event as CustomEvent).detail;
    if (detail?.successful === false) return;
    hide();
  });
}

type LiveUpdateState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected';

let relayRunEventSource: EventSource | null = null;
let relayRunEventSourceUrl = '';
let relayRunEventShell: HTMLElement | null = null;
let relayRunRefreshTimer: number | undefined;
let relayRunRefreshInFlight = false;
let relayRunRefreshPending = false;

function currentWorkbenchShell(): HTMLElement | null {
  return document.querySelector<HTMLElement>('[data-relay-workbench][data-relay-run-events]');
}

function liveUpdatesIndicator(): HTMLElement | null {
  return document.querySelector<HTMLElement>('[data-relay-live-updates-indicator]');
}

function liveUpdatesIndicatorText(): HTMLElement | null {
  return document.querySelector<HTMLElement>('[data-relay-live-updates-text]');
}

function setLiveUpdatesIndicator(state: LiveUpdateState): void {
  const indicator = liveUpdatesIndicator();
  if (!indicator) return;
  const text = liveUpdatesIndicatorText();

  indicator.dataset.relayLiveUpdatesState = state;
  indicator.classList.remove('border-green-700', 'border-yellow-700', 'border-red-700', 'text-green-300', 'text-yellow-300', 'text-red-300', 'bg-green-950/60', 'bg-yellow-950/60', 'bg-red-950/60');

  indicator.querySelectorAll<HTMLElement>('[data-relay-live-updates-state-icon]').forEach((icon) => {
    icon.hidden = icon.dataset.relayLiveUpdatesStateIcon !== state;
  });

  switch (state) {
    case 'connected':
      if (text) text.textContent = 'Live updates connected';
      indicator.classList.add('border-green-700', 'text-green-300', 'bg-green-950/60');
      break;
    case 'reconnecting':
      if (text) text.textContent = 'Live updates reconnecting';
      indicator.classList.add('border-yellow-700', 'text-yellow-300', 'bg-yellow-950/60');
      break;
    case 'disconnected':
      if (text) text.textContent = 'Live updates disconnected - refresh to resync';
      indicator.classList.add('border-red-700', 'text-red-300', 'bg-red-950/60');
      break;
    default:
      if (text) text.textContent = 'Live updates connecting';
      indicator.classList.add('border-gray-700', 'text-gray-400', 'bg-gray-950/70');
      break;
  }
}

function closeRunEventSource(): void {
  if (relayRunEventSource) {
    relayRunEventSource.close();
  }
  relayRunEventSource = null;
  relayRunEventSourceUrl = '';
  relayRunEventShell = null;
}

function queueWorkbenchRefresh(): void {
  if (relayRunRefreshTimer != null) return;
  relayRunRefreshTimer = window.setTimeout(() => {
    relayRunRefreshTimer = undefined;
    if (relayRunRefreshInFlight) {
      relayRunRefreshPending = true;
      return;
    }

    const shell = currentWorkbenchShell();
    const url = shell?.getAttribute('data-relay-run-url') || '';
    if (!shell || !url) {
      relayRunRefreshPending = false;
      return;
    }

    relayRunRefreshInFlight = true;
    relayRunRefreshPending = false;

    htmx.ajax('GET', url, {
      target: '#run-workbench-shell',
      select: '#run-workbench-shell',
      swap: 'outerHTML show:#run-workbench-shell:top settle:120ms',
    });
  }, 250);
}

function flushWorkbenchRefreshQueue(): void {
  if (!relayRunRefreshPending || relayRunRefreshInFlight) return;
  relayRunRefreshPending = false;
  queueWorkbenchRefresh();
}

function hasReusableRunEventSource(url: string): boolean {
  return relayRunEventSource !== null
    && relayRunEventSourceUrl === url
    && relayRunEventSource.readyState !== EventSource.CLOSED;
}

function initRunEventStream(): void {
  const shell = currentWorkbenchShell();
  if (!shell) {
    closeRunEventSource();
    setLiveUpdatesIndicator('disconnected');
    return;
  }

  const url = shell.getAttribute('data-relay-run-events') || '';
  if (!url) {
    closeRunEventSource();
    setLiveUpdatesIndicator('disconnected');
    return;
  }

  // HTMX replaces the shell element during a refresh, so keep the stream if the URL is unchanged.
  if (hasReusableRunEventSource(url)) {
    relayRunEventShell = shell;
    return;
  }

  closeRunEventSource();
  relayRunEventShell = shell;
  relayRunEventSourceUrl = url;
  setLiveUpdatesIndicator('connecting');

  if (typeof EventSource === 'undefined') {
    setLiveUpdatesIndicator('disconnected');
    return;
  }

  const source = new EventSource(url);
  relayRunEventSource = source;

  const connectionEvents = ['run.connected'];
  const refreshEvents = ['run.summary', 'step.agent', 'step.validation', 'step.audit', 'step.commit', 'step.artifacts', 'toast'];

  source.onopen = () => {
    setLiveUpdatesIndicator('connected');
  };

  connectionEvents.forEach((name) => {
    source.addEventListener(name, () => {
      setLiveUpdatesIndicator('connected');
    });
  });

  refreshEvents.forEach((name) => {
    source.addEventListener(name, () => {
      queueWorkbenchRefresh();
    });
  });

  source.onerror = () => {
    if (!relayRunEventSource) {
      return;
    }
    if (source.readyState === EventSource.CLOSED) {
      setLiveUpdatesIndicator('disconnected');
    } else {
      setLiveUpdatesIndicator('reconnecting');
    }
  };
}

initDelegatedCopyControls();
initWorkbenchSwapFocus();
initWorkbenchBusyIndicator();
initRelayActionSubmitState();
initArtifactPreviewControls();
initHTMXErrorBanner();
initRunEventStream();

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
