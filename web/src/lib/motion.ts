import type { Transition } from 'framer-motion'

// Shared by every stagger-in list (Jobs/Dashboard/Events/Snapshots row
// animations) so the reduced-motion override is applied consistently
// rather than re-derived per page. The delay cap keeps a long list (e.g.
// 200 events) from taking seconds to finish animating in.
export function staggerTransition(reduceMotion: boolean, index: number, cap = 12): Transition {
  if (reduceMotion) return { duration: 0 }
  return { duration: 0.15, delay: Math.min(index, cap) * 0.02 }
}
