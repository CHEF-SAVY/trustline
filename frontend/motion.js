// Scroll choreography.
//
// Three jobs: reveal elements as they enter view, drive the progress hairline,
// and flag the header once the page has moved. All of it is transform/opacity
// only — nothing here can trigger layout.
//
// Fail-visible by construction. Content is visible in CSS by default; this
// script adds `.motion-ready` to <html>, which is what arms the hiding. So if
// this file never loads — blocked module, CSP rule, parse error — the page is
// simply static and fully readable, never blank.
//
// (Learned the hard way: the first version hid content in CSS and revealed it
// with JS. Opened over file://, ES modules are blocked by CORS, and the whole
// hero rendered empty.)

const REVEAL_SELECTOR = '[data-reveal]';

/** Reveal everything immediately — used for reduced-motion and unsupported browsers. */
function revealAll() {
  document.querySelectorAll(REVEAL_SELECTOR).forEach((el) => el.classList.add('in'));
}

function initReveals() {
  const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  if (prefersReduced || !('IntersectionObserver' in window)) {
    // Leave content visible; never arm .motion-ready.
    revealAll();
    return;
  }

  // Arm the hiding only now that we know we can animate it back in.
  document.documentElement.classList.add('motion-ready');

  // Anything already in view on load should come in immediately rather than
  // waiting for a scroll that may never happen on a short screen.

  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;

        // Per-element opt-in delay, for the few places where a beat reads better
        // than a simultaneous reveal (hero annotation trailing the headline).
        const delay = Number(entry.target.dataset.delay || 0);
        if (delay > 0) {
          setTimeout(() => entry.target.classList.add('in'), delay);
        } else {
          entry.target.classList.add('in');
        }

        // Reveal once. Re-animating on scroll-back is a common tic that makes a
        // page feel restless and makes it slower to re-read.
        observer.unobserve(entry.target);
      }
    },
    {
      // Fire slightly before the element reaches the viewport edge, so motion
      // completes around the point the user's eye arrives.
      //
      // threshold MUST stay 0. An earlier version used 0.15, which asks for 15%
      // of the ELEMENT to be inside an already-shrunk root — a tall block near
      // the end of the page can never satisfy both at once, so it stayed at
      // opacity 0 permanently. That stranded 4 of 20 elements, including the
      // whole privacy caveat. Any sliver entering is enough to trigger.
      rootMargin: '0px 0px -8% 0px',
      threshold: 0,
    }
  );

  const items = Array.from(document.querySelectorAll(REVEAL_SELECTOR));
  items.forEach((el) => observer.observe(el));

  // Safety net. The observer alone can strand content that is on screen but
  // never "enters" — short pages that don't scroll, anchor jumps that skip past
  // an element, or a viewport taller than the remaining scroll distance.
  // Anything whose top is already above the fold gets revealed outright.
  const sweep = () => {
    const h = window.innerHeight;
    let remaining = 0;
    for (const el of items) {
      if (el.classList.contains('in')) continue;
      if (el.getBoundingClientRect().top < h) {
        el.classList.add('in');
        observer.unobserve(el);
      } else {
        remaining++;
      }
    }
    if (remaining === 0) window.removeEventListener('scroll', onScrollSweep);
  };
  let sweepQueued = false;
  const onScrollSweep = () => {
    if (sweepQueued) return;
    sweepQueued = true;
    requestAnimationFrame(() => { sweepQueued = false; sweep(); });
  };

  window.addEventListener('scroll', onScrollSweep, { passive: true });
  window.addEventListener('resize', onScrollSweep, { passive: true });
  window.addEventListener('load', sweep);
  sweep();
}

function initScrollUI() {
  const progress = document.getElementById('progress');
  const head = document.querySelector('.site-head');
  let ticking = false;

  const update = () => {
    const doc = document.documentElement;
    const max = doc.scrollHeight - window.innerHeight;
    const ratio = max > 0 ? Math.min(window.scrollY / max, 1) : 0;

    if (progress) progress.style.transform = `scaleX(${ratio})`;
    if (head) head.classList.toggle('is-stuck', window.scrollY > 8);

    ticking = false;
  };

  // rAF-throttled: scroll fires far more often than we can usefully paint, and
  // reading scrollHeight every event is a layout read in a hot path.
  const onScroll = () => {
    if (ticking) return;
    ticking = true;
    requestAnimationFrame(update);
  };

  window.addEventListener('scroll', onScroll, { passive: true });
  window.addEventListener('resize', onScroll, { passive: true });
  update();
}

initReveals();
initScrollUI();
