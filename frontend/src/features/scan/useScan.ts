import { useCallback, useEffect, useState } from 'react'
import { CancelScan, StartFastScan, StartScan } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

// Event names must match internal/app/app.go.
const EVENT_PROGRESS = 'scan:progress'
const EVENT_DONE = 'scan:done'
const EVENT_ERROR = 'scan:error'
const EVENT_CANCELLED = 'scan:cancelled'
const EVENT_DECLINED = 'scan:declined'

export type ScanProgress = {
  files: number
  dirs: number
  bytes: number
  errors: number
  path: string
  elapsedMs: number
}

export type ScanState =
  | { status: 'idle' }
  | { status: 'scanning'; progress: ScanProgress | null; fast: boolean }
  | { status: 'done'; root: app.NodeDTO; scanId: number }
  | { status: 'cancelled' }
  | { status: 'declined' }
  | { status: 'error'; error: string }

export function useScan() {
  const [state, setState] = useState<ScanState>({ status: 'idle' })

  useEffect(() => {
    const offs = [
      EventsOn(EVENT_PROGRESS, (p: ScanProgress) =>
        setState((s) => (s.status === 'scanning' ? { ...s, progress: p } : s)),
      ),
      EventsOn(EVENT_DONE, (root: app.NodeDTO) => setState({ status: 'done', root, scanId: Date.now() })),
      EventsOn(EVENT_ERROR, (error: string) => setState({ status: 'error', error })),
      EventsOn(EVENT_CANCELLED, () => setState({ status: 'cancelled' })),
      EventsOn(EVENT_DECLINED, () => setState({ status: 'declined' })),
    ]
    return () => offs.forEach((off) => off())
  }, [])

  const start = useCallback(async (path: string, fast = false) => {
    setState({ status: 'scanning', progress: null, fast })
    try {
      await (fast ? StartFastScan(path) : StartScan(path))
    } catch (e) {
      setState({ status: 'error', error: String(e) })
    }
  }, [])

  const cancel = useCallback(() => {
    void CancelScan()
  }, [])

  return { state, start, cancel }
}
