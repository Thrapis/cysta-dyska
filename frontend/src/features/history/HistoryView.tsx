import { useEffect, useMemo, useState } from 'react'
import { ChevronRight, File, Folder } from 'lucide-react'
import { CompareChildren, History, Hotspots } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useI18n } from '@/i18n'
import { formatBytes, formatDelta } from '@/lib/format'
import { cn } from 'cn'

/** Compares the current scan with an earlier scan of the same folder. */
export function HistoryView({ scanId }: { scanId: number }) {
  const { t, locale } = useI18n()
  const [history, setHistory] = useState<app.HistoryDTO | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [baseline, setBaseline] = useState<number>(0)

  useEffect(() => {
    let live = true
    History()
      .then((h) => {
        if (!live) return
        setHistory(h)
        // Default: the scan just before the current one.
        const older = h.snapshots.filter((s) => s.id !== h.currentId)
        setBaseline(older[0]?.id ?? 0)
      })
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [scanId])

  const dateFmt = useMemo(() => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }), [locale])

  if (error) return <p className="text-sm text-destructive">{error}</p>
  if (!history) return null
  if (!history.currentId) {
    return <p className="text-sm text-muted-foreground">{t('historyUnavailable', { warning: history.warning ?? '' })}</p>
  }

  const current = history.snapshots.find((s) => s.id === history.currentId)
  const older = history.snapshots.filter((s) => s.id !== history.currentId)
  const base = older.find((s) => s.id === baseline)
  const minSize = formatBytes(history.minAlloc, locale)

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">{t('historyNote', { size: minSize })}</p>
      {older.length === 0 || !current ? (
        <p className="text-sm">{t('historyFirst')}</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <label htmlFor="baseline">{t('compareWith')}</label>
            <select
              id="baseline"
              className="rounded-md border bg-background px-2 py-1"
              value={baseline}
              onChange={(e) => setBaseline(Number(e.target.value))}
            >
              {older.map((s) => (
                <option key={s.id} value={s.id}>
                  {dateFmt.format(new Date(s.takenAt))} · {formatBytes(s.alloc, locale)}
                </option>
              ))}
            </select>
          </div>
          {base && (
            <>
              <Summary before={base} after={current} date={dateFmt.format(new Date(base.takenAt))} />
              <HotspotList before={base.id} after={current.id} minDelta={history.hotspotMin} />
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{t('allChanges')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <DiffTable before={base.id} after={current.id} minSize={minSize} />
                </CardContent>
              </Card>
            </>
          )}
        </>
      )}
    </div>
  )
}

function Summary({ before, after, date }: { before: app.SnapshotDTO; after: app.SnapshotDTO; date: string }) {
  const { t, locale } = useI18n()
  const delta = after.alloc - before.alloc
  const small = Math.abs(delta) < 1 << 20
  const key = small ? 'historySame' : delta > 0 ? 'historyGrew' : 'historyShrank'
  return (
    <p className={cn('text-base font-medium', !small && delta > 0 && 'text-amber-600 dark:text-amber-400')}>
      {t(key, { delta: formatBytes(Math.abs(delta), locale), date })}
    </p>
  )
}

function HotspotList({ before, after, minDelta }: { before: number; after: number; minDelta: number }) {
  const { t, locale } = useI18n()
  const [rows, setRows] = useState<app.DiffRowDTO[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    Hotspots(before, after)
      .then((r) => live && setRows(r))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [before, after])

  const size = formatBytes(minDelta, locale)
  return (
    <Card>
      <CardHeader className="gap-1">
        <CardTitle className="text-base">{t('hotspots')}</CardTitle>
        <p className="text-xs text-muted-foreground">{t('hotspotsHint', { size })}</p>
      </CardHeader>
      <CardContent className="space-y-1">
        {error && <p className="text-sm text-destructive">{error}</p>}
        {rows?.length === 0 && <p className="text-sm text-muted-foreground">{t('hotspotsNone', { size })}</p>}
        {rows?.map((r) => (
          <div key={r.key} className="flex items-center gap-2 text-sm">
            <DeltaText delta={r.delta} className="w-28 shrink-0 text-right" />
            <code className="min-w-0 flex-1 truncate text-xs" title={r.path || '.'}>
              {r.path || '.'}
            </code>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function DeltaText({ delta, className }: { delta: number; className?: string }) {
  const { locale } = useI18n()
  return (
    <span
      className={cn(
        'tabular-nums',
        delta > 0 && 'text-amber-600 dark:text-amber-400',
        delta < 0 && 'text-emerald-600 dark:text-emerald-400',
        className,
      )}
    >
      {formatDelta(delta, locale)}
    </span>
  )
}

function DiffTable({ before, after, minSize }: { before: number; after: number; minSize: string }) {
  const { t } = useI18n()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('name')}</TableHead>
          <TableHead className="w-28 text-right">{t('before')}</TableHead>
          <TableHead className="w-28 text-right">{t('now')}</TableHead>
          <TableHead className="w-28 text-right">{t('change')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <DiffLevel key={`${before}-${after}`} before={before} after={after} parent="" depth={0} minSize={minSize} />
      </TableBody>
    </Table>
  )
}

type LevelProps = { before: number; after: number; parent: string; depth: number; minSize: string }

function DiffLevel({ before, after, parent, depth, minSize }: LevelProps) {
  const [rows, setRows] = useState<app.DiffRowDTO[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    CompareChildren(before, after, parent)
      .then((r) => live && setRows(r))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [before, after, parent])

  if (error) {
    return (
      <TableRow>
        <TableCell colSpan={4} className="text-destructive">
          {error}
        </TableCell>
      </TableRow>
    )
  }
  return (
    <>
      {rows?.map((r) => (
        <DiffRow key={r.key} row={r} before={before} after={after} depth={depth} minSize={minSize} />
      ))}
    </>
  )
}

function DiffRow({ row, before, after, depth, minSize }: { row: app.DiffRowDTO } & Omit<LevelProps, 'parent'>) {
  const { t, locale } = useI18n()
  const [open, setOpen] = useState(false)
  const name = row.path.slice(row.path.lastIndexOf('\\') + 1)
  const Icon = row.isDir ? Folder : File
  const size = (known: boolean, bytes: number) => (known ? formatBytes(bytes, locale) : t('belowThreshold', { size: minSize }))

  return (
    <>
      <TableRow>
        <TableCell className="max-w-0" style={{ paddingLeft: `${0.5 + depth * 1.25}rem` }}>
          <div className="flex min-w-0 items-center gap-1.5">
            {row.hasChildren ? (
              <button
                type="button"
                className="rounded p-0.5 hover:bg-muted"
                aria-expanded={open}
                aria-label={t(open ? 'collapse' : 'expand', { name })}
                onClick={() => setOpen(!open)}
              >
                <ChevronRight className={cn('size-4 transition-transform', open && 'rotate-90')} />
              </button>
            ) : (
              <span className="w-5 shrink-0" />
            )}
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate" title={row.path}>
              {name}
            </span>
          </div>
        </TableCell>
        <TableCell className="text-right tabular-nums text-muted-foreground">{size(row.beforeKnown, row.before)}</TableCell>
        <TableCell className="text-right tabular-nums">{size(row.afterKnown, row.after)}</TableCell>
        <TableCell className="text-right">
          <DeltaText delta={row.delta} />
        </TableCell>
      </TableRow>
      {open && <DiffLevel before={before} after={after} parent={row.key} depth={depth + 1} minSize={minSize} />}
    </>
  )
}
