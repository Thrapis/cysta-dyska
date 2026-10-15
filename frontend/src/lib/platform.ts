import { useEffect, useState } from 'react'
import { Platform } from '../../wailsjs/go/app/App'

let cached: Promise<string> | undefined

/** The OS the backend runs on ("windows" or "darwin"); "" until it is known. */
export function usePlatform(): string {
  const [platform, setPlatform] = useState('')
  useEffect(() => {
    cached ??= Platform().catch(() => '')
    void cached.then(setPlatform)
  }, [])
  return platform
}
