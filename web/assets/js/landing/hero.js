// Loads the 3D mascot, but only when it is worth loading.
//
// three.js is ~700 KB across two files. Fetching that for a visitor who bounces
// off the hero, or who has asked for reduced motion, or whose browser cannot
// give us a WebGL context, is pure waste — so every one of those cases is
// checked before the dynamic import, not after.
//
// The static SVG mark underneath is the real logo. This only ever replaces it,
// and only once it has something to show.

const CANVAS_SELECTOR = '[data-mascot-canvas]';
const FALLBACK_SELECTOR = '[data-mascot-fallback]';

function prefersReducedMotion() {
  return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
}

// hasWebGL asks for a context rather than sniffing the user agent. A browser
// with WebGL disabled, or a machine that has exhausted its context limit, both
// answer honestly here and nowhere else.
function hasWebGL() {
  try {
    const probe = document.createElement('canvas');
    const gl = probe.getContext('webgl2') || probe.getContext('webgl');
    if (!gl) return false;
    gl.getExtension('WEBGL_lose_context')?.loseContext();
    return true;
  } catch {
    return false;
  }
}

function init() {
  const canvas = document.querySelector(CANVAS_SELECTOR);
  if (!canvas) return;

  const fallback = document.querySelector(FALLBACK_SELECTOR);
  const reducedMotion = prefersReducedMotion();

  if (!hasWebGL()) return; // The SVG stays; nothing else to do.

  let mascot = null;
  let loading = false;

  const reveal = () => {
    canvas.classList.remove('opacity-0');
    if (fallback) fallback.classList.add('hidden');
  };

  const load = async () => {
    if (mascot || loading) return;
    loading = true;

    try {
      const { createMascot } = await import('/assets/js/landing/mascot.js');
      mascot = createMascot(canvas, { reducedMotion });
      reveal();
      mascot.play();
    } catch (err) {
      // A failed import is not worth breaking the page over — the SVG is still
      // sitting there, which is why it was never removed until now.
      console.warn('[seshat] mascot unavailable', err);
    } finally {
      loading = false;
    }
  };

  // Only load once the canvas is actually near the viewport.
  const visibility = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          load();
          // Animating something scrolled off screen burns battery to render
          // frames nobody sees.
          mascot?.play();
        } else {
          mascot?.pause();
        }
      }
    },
    { rootMargin: '200px' },
  );
  visibility.observe(canvas);

  // Same reasoning for a backgrounded tab. requestAnimationFrame already
  // throttles there, but it is not guaranteed to stop.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) mascot?.pause();
    else mascot?.play();
  });

  // htmx swaps the body content on navigation, which would leave the WebGL
  // context attached to a canvas no longer in the document.
  document.body.addEventListener('htmx:before:swap', () => {
    if (canvas.isConnected) return;
    mascot?.dispose();
    mascot = null;
  });
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
