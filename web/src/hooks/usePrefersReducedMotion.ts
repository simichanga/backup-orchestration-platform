import { useEffect, useState } from 'react'

// framer-motion's own MotionConfig reducedMotion="user" only disables
// transform-based animation (x/y/scale/rotate) under prefers-reduced-motion
// - it deliberately still animates opacity, which doesn't match this
// project's stricter policy (see global.css: every CSS animation/transition
// duration collapses to ~0 under the same media query, no exceptions).
// Read the preference directly instead and zero out transition duration
// explicitly wherever a motion.* component is used, so JS-driven and
// CSS-driven motion are held to the same standard.
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() => window.matchMedia('(prefers-reduced-motion: reduce)').matches)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const onChange = () => setReduced(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  return reduced
}
