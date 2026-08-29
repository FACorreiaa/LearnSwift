// The Seshat mascot: the seven-pointed star of her headdress, extruded into
// three dimensions, with a swift's swept wings folded back from it.
//
// This module is imported dynamically by hero.js, and only when the mascot is
// about to be seen. three.js is ~700 KB across its two files, and a visitor who
// never scrolls to the hero should never pay for it.

import {
  AmbientLight,
  DirectionalLight,
  ExtrudeGeometry,
  Group,
  Mesh,
  MeshStandardMaterial,
  PerspectiveCamera,
  Scene,
  Shape,
  WebGLRenderer,
} from '/assets/js/vendor/three.module.min.js';

const POINTS = 7; // Seshat's headdress
const OUTER = 1.0;
const INNER = 0.42;

// sevenPointedStar builds the outline once, in 2D, and lets ExtrudeGeometry give
// it depth. Describing the solid directly would mean generating side walls and
// bevels by hand for no gain.
function sevenPointedStar() {
  const shape = new Shape();

  for (let i = 0; i < POINTS * 2; i++) {
    const radius = i % 2 === 0 ? OUTER : INNER;
    // -PI/2 so a point faces up rather than the flat between two points.
    const angle = (i / (POINTS * 2)) * Math.PI * 2 - Math.PI / 2;
    const x = Math.cos(angle) * radius;
    const y = Math.sin(angle) * radius;

    if (i === 0) shape.moveTo(x, y);
    else shape.lineTo(x, y);
  }
  shape.closePath();

  return new ExtrudeGeometry(shape, {
    depth: 0.16,
    bevelEnabled: true,
    bevelThickness: 0.04,
    bevelSize: 0.035,
    bevelSegments: 3,
    curveSegments: 12,
  });
}

// wing is a swept quadrilateral — a swift's silhouette is a scythe, not a
// feathered fan, which is what makes it readable at this size.
function wing(direction) {
  const shape = new Shape();
  shape.moveTo(0, 0);
  shape.quadraticCurveTo(1.5 * direction, 0.35, 2.6 * direction, -0.15);
  shape.quadraticCurveTo(1.6 * direction, -0.1, 0.15 * direction, -0.42);
  shape.closePath();

  return new ExtrudeGeometry(shape, {
    depth: 0.05,
    bevelEnabled: true,
    bevelThickness: 0.015,
    bevelSize: 0.015,
    bevelSegments: 2,
    curveSegments: 12,
  });
}

export function createMascot(canvas, options = {}) {
  const { reducedMotion = false } = options;

  const renderer = new WebGLRenderer({
    canvas,
    // The page background shows through, so the mascot sits on whatever theme
    // is active rather than carrying its own panel.
    alpha: true,
    antialias: true,
  });
  renderer.setClearAlpha(0);

  const scene = new Scene();

  const camera = new PerspectiveCamera(38, 1, 0.1, 100);
  camera.position.set(0, 0, 6.4);

  const gold = new MeshStandardMaterial({ color: 0xd9a441, roughness: 0.32, metalness: 0.72 });
  const ink = new MeshStandardMaterial({ color: 0x8a6a2f, roughness: 0.55, metalness: 0.45 });

  const mascot = new Group();

  const star = new Mesh(sevenPointedStar(), gold);
  star.position.z = 0.05;
  mascot.add(star);

  for (const direction of [1, -1]) {
    const w = new Mesh(wing(direction), ink);
    w.position.set(direction * 0.55, -0.1, -0.12);
    w.rotation.z = direction * -0.22;
    mascot.add(w);
  }

  // A slight resting tilt, so the extrusion reads as depth even in the first
  // frame — and in the still frame a reduced-motion visitor gets.
  mascot.rotation.set(0.18, -0.34, 0);
  scene.add(mascot);

  scene.add(new AmbientLight(0xffffff, 1.5));

  const key = new DirectionalLight(0xffffff, 2.4);
  key.position.set(2.5, 3, 4);
  scene.add(key);

  const rim = new DirectionalLight(0x9ecbff, 1.1);
  rim.position.set(-3, -1.5, -2);
  scene.add(rim);

  // resize reads the element's CSS size and matches the drawing buffer to it.
  // Without the devicePixelRatio step the mascot is soft on every retina
  // display; the cap at 2 is because beyond it the cost is real and the
  // improvement is not.
  function resize() {
    const width = canvas.clientWidth || 1;
    const height = canvas.clientHeight || 1;

    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.setSize(width, height, false);

    camera.aspect = width / height;
    camera.updateProjectionMatrix();
  }

  const observer = new ResizeObserver(resize);
  observer.observe(canvas);
  resize();

  let frame = 0;
  let running = false;
  let start = 0;

  function render() {
    renderer.render(scene, camera);
  }

  function tick(now) {
    if (!running) return;
    if (!start) start = now;
    const t = (now - start) / 1000;

    // A slow figure-of-eight rather than a spin. A continuously rotating logo
    // reads as a loading spinner, which is the opposite of what this is for.
    mascot.rotation.y = -0.34 + Math.sin(t * 0.55) * 0.42;
    mascot.rotation.x = 0.18 + Math.sin(t * 0.37) * 0.12;
    mascot.position.y = Math.sin(t * 0.7) * 0.06;

    render();
    frame = requestAnimationFrame(tick);
  }

  function play() {
    if (running || reducedMotion) return;
    running = true;
    start = 0;
    frame = requestAnimationFrame(tick);
  }

  function pause() {
    running = false;
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
  }

  function dispose() {
    pause();
    observer.disconnect();
    // three.js allocates GPU buffers that garbage collection cannot reach, so
    // they have to be released by hand or navigating away leaks them.
    scene.traverse((object) => {
      if (object.geometry) object.geometry.dispose();
      if (object.material) object.material.dispose();
    });
    renderer.dispose();
  }

  // Draw once regardless, so a reduced-motion visitor gets a composed still
  // rather than an empty canvas.
  render();

  return { play, pause, dispose };
}
