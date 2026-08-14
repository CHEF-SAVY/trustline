// Lightweight page behaviour only: simple one-shot reveals, the reading
// progress hairline, and the sticky-header boundary. Content is visible by
// default; JavaScript opts into hiding only after IntersectionObserver exists.

const items = Array.from(document.querySelectorAll('[data-reveal]'));
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
let revealObserver = null;

function revealVisible() {
  for (const item of items) {
    if (item.classList.contains('in')) continue;
    if (item.getBoundingClientRect().top < window.innerHeight) {
      item.classList.add('in');
      revealObserver?.unobserve(item);
    }
  }
}

if (!reduced && 'IntersectionObserver' in window && items.length) {
  document.documentElement.classList.add('motion-ready');

  revealObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      entry.target.classList.add('in');
      revealObserver.unobserve(entry.target);
    }
  }, { rootMargin: '0px 0px -6% 0px', threshold: 0 });

  items.forEach((item) => revealObserver.observe(item));

  // Do not strand content after anchor jumps or unusually tall viewports.
  window.addEventListener('load', revealVisible, { once: true });
  window.addEventListener('resize', revealVisible, { passive: true });
  revealVisible();
} else {
  items.forEach((item) => item.classList.add('in'));
}

const progress = document.getElementById('progress');
const header = document.querySelector('.site-head');
let queued = false;

function paintScrollState() {
  const root = document.documentElement;
  const distance = root.scrollHeight - window.innerHeight;
  const ratio = distance > 0 ? Math.min(window.scrollY / distance, 1) : 0;
  if (progress) progress.style.transform = `scaleX(${ratio})`;
  if (header) header.classList.toggle('is-stuck', window.scrollY > 8);
  revealVisible();
  queued = false;
}

function queuePaint() {
  if (queued) return;
  queued = true;
  requestAnimationFrame(paintScrollState);
}

window.addEventListener('scroll', queuePaint, { passive: true });
window.addEventListener('resize', queuePaint, { passive: true });
paintScrollState();
