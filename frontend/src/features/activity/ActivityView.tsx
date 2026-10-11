import { useEffect, useMemo, useState } from 'react'
import { ActionLog } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Badge } from '@/components/ui/badge'
import { useI18n, type MessageKey } from '@/i18n'
import { formatBytes } from '@/lib/format'

/** The log of every action Čysta Dyska carried out, newest first. */
export function ActivityView({ refresh }: { refresh: number }) {
  const { t, locale } = useI18n()
  const [log, setLog] = useState<app.ActionLogDTO[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const dateFmt = useMemo(() => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }), [locale])

  useEffect(() => {
    let live = true
    ActionLog()
      .then((l) => live && setLog(l))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [refresh])

  if (error) return <p className="text-sm text-destructive">{error}</p>
  if (!log) return null
  if (log.length === 0) return <p className="text-sm text-muted-foreground">{t('activityEmpty')}</p>

  return (
    <ul className="space-y-2">
      {log.map((e) => (
        <li key={e.id} className="rounded-md border p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{e.owner}</span>
            <span className="text-muted-foreground">{t(`kind_${e.kind}` as MessageKey)}</span>
            <Badge variant={e.outcome === 'done' ? 'secondary' : 'outline'}>{e.outcome}</Badge>
            {e.outcome === 'done' && e.bytesBefore > e.bytesAfter && (
              <span className="text-emerald-600 dark:text-emerald-400">
                {t(e.kind === 'recycle' ? 'activityRecycled' : 'activityFreed', {
                  size: formatBytes(e.bytesBefore - e.bytesAfter, locale),
                })}
              </span>
            )}
            <span className="ml-auto text-xs text-muted-foreground">{dateFmt.format(new Date(e.at))}</span>
          </div>
          <code className="mt-1 block truncate text-xs" title={e.target}>
            {e.command || e.target}
          </code>
          {e.detail && (
            <details>
              <summary className="cursor-pointer text-xs text-muted-foreground">{t('showOutput')}</summary>
              <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">{e.detail}</pre>
            </details>
          )}
        </li>
      ))}
    </ul>
  )
}
