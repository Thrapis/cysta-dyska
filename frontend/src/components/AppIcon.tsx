import { useEffect, useState } from 'react'
import { Package } from 'lucide-react'
import { AppIcons } from '../../wailsjs/go/app/App'
import { cn } from 'cn'

// Icons are looked up by app name in one batched call and kept for the life of
// the page. '' means "asked, none found", so the placeholder is shown and the
// backend is not asked again.
const known = new Map<string, string>()
const queued = new Set<string>()
const listeners = new Set<() => void>()
let timer: ReturnType<typeof setTimeout> | undefined

function request(owner: string) {
  if (known.has(owner) || queued.has(owner)) return
  queued.add(owner)
  timer ??= setTimeout(flush, 30)
}

async function flush() {
  timer = undefined
  const owners = [...queued]
  queued.clear()
  let found: Record<string, string> = {}
  try {
    found = (await AppIcons(owners)) ?? {}
  } catch {
    // No icons is not an error worth showing: the placeholder stays.
  }
  for (const o of owners) known.set(o, found[o] ?? '')
  listeners.forEach((l) => l())
}

/** An app's icon, or a neutral placeholder when it has none. */
export function AppIcon({ owner, className }: { owner: string; className?: string }) {
  const [, setTick] = useState(0)
  useEffect(() => {
    const l = () => setTick((n) => n + 1)
    listeners.add(l)
    request(owner)
    return () => {
      listeners.delete(l)
    }
  }, [owner])

  const src = known.get(owner)
  if (src) return <img src={src} alt="" aria-hidden className={cn('size-5 shrink-0', className)} />
  return <Package aria-hidden className={cn('size-5 shrink-0 text-muted-foreground', className)} />
}
