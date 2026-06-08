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
