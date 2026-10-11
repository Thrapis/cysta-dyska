import { useEffect, useRef, useState } from 'react'
import { ArrowUp } from 'lucide-react'
import { Treemap } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/i18n'
import { formatBytes } from '@/lib/format'
import { cn } from 'cn'

// Tile colours cycle through these; folders are stronger than files.
const HUES = [210, 160, 30, 280, 340, 100, 190, 50]

/** Squarified treemap of one folder level; click a folder to open it. */
export function TreemapView({ root, scanId }: { root: app.NodeDTO; scanId: number }) {
  const { t, locale, formatNumber } = useI18n()
  const box = useRef<HTMLDivElement>(null)
  const [path, setPath] = useState(root.path)
  const [size, setSize] = useState({ w: 0, h: 0 })
  const [tiles, setTiles] = useState<app.TileDTO[]>([])
  const [error, setError] = useState<string | null>(null)

  // Track the container size, debounced so resizing stays smooth.
  useEffect(() => {
    const el = box.current
    if (!el) return
    let timer: ReturnType<typeof setTimeout>
    const ro = new ResizeObserver(([e]) => {
      clearTimeout(timer)
      timer = setTimeout(() => setSize({ w: e.contentRect.width, h: e.contentRect.height }), 80)
    })
    ro.observe(el)
    return () => {
      clearTimeout(timer)
      ro.disconnect()
    }
  }, [])

  useEffect(() => {
    if (size.w <= 0 || size.h <= 0) return
    let live = true
    Treemap(path, size.w, size.h)
      .then((ts) => {
        if (!live) return
        setTiles(ts)
        setError(null)
      })
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [path, size, scanId])

  const atRoot = path.toLowerCase() === root.path.toLowerCase()
  const up = () => {
    if (atRoot) return
    const parent = path.slice(0, path.lastIndexOf('\\'))
    // Never go above the scanned folder; this also avoids "C:" for "C:\".
    setPath(parent.length <= root.path.length ? root.path : parent)
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button size="sm" variant="outline" disabled={atRoot} onClick={up}>
          <ArrowUp /> {t('mapUp')}
        </Button>
        <code className="min-w-0 flex-1 truncate text-xs" title={path}>
          {path}
        </code>
        <span className="text-xs text-muted-foreground">{t('mapHint')}</span>
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}
      <div ref={box} className="relative h-[60vh] min-h-64 w-full overflow-hidden rounded-md border bg-muted/30">
        {tiles.length === 0 && !error && size.w > 0 && (
          <p className="p-4 text-sm text-muted-foreground">{t('mapEmpty')}</p>
        )}
        {tiles.map((tile, i) => {
          const hue = HUES[i % HUES.length]
          const clickable = tile.isDir && tile.hasChildren && !tile.isRest
          const label = tile.isRest ? t('mapOther', { count: formatNumber(tile.count) }) : tile.name
          const showText = tile.w > 60 && tile.h > 28
          return (
            <button
              key={tile.isRest ? '__rest__' : tile.path}
              type="button"
              disabled={!clickable}
              onClick={() => clickable && setPath(tile.path)}
              title={`${label}\n${formatBytes(tile.alloc, locale)}`}
              className={cn(
                'absolute flex flex-col items-start justify-start overflow-hidden border border-background p-1 text-left text-xs text-white',
                clickable ? 'cursor-pointer hover:brightness-110' : 'cursor-default',
              )}
              style={{
                left: tile.x,
                top: tile.y,
                width: tile.w,
                height: tile.h,
                background: tile.isRest
                  ? 'hsl(0 0% 45%)'
                  : `hsl(${hue} ${tile.isDir ? 55 : 30}% ${tile.isDir ? 38 : 48}%)`,
              }}
            >
              {showText && (
                <>
                  <span className="block truncate font-medium">{label}</span>
                  <span className="block truncate opacity-80">{formatBytes(tile.alloc, locale)}</span>
                </>
              )}
            </button>
          )
        })}
      </div>
    </div>
  )
}
