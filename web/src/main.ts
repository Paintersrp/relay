import htmx from 'htmx.org';
import Alpine from 'alpinejs';

(window as any).htmx = htmx;

(window as any).Alpine = Alpine;
Alpine.start();

document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('[data-copy-target]').forEach((el) => {
    el.addEventListener('click', () => {
      const targetId = el.getAttribute('data-copy-target');
      if (!targetId) return;
      const target = document.getElementById(targetId);
      if (!target) return;
      const text = target.textContent || '';
      navigator.clipboard.writeText(text).then(() => {
        const orig = el.textContent || '';
        el.textContent = 'Copied!';
        setTimeout(() => { el.textContent = orig; }, 1500);
      }).catch(() => {});
    });
  });
});

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
