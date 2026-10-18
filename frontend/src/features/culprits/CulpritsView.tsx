import { useEffect, useMemo, useState } from 'react'
import { Check, Copy, FolderSearch, Settings } from 'lucide-react'
import { Culprits } from '../../../wailsjs/go/app/App'
import { OpenSettings, Reveal } from '../../../wailsjs/go/main/Shell'
import type { app, attribute } from '../../../wailsjs/go/models'
import { ClipboardSetText } from '../../../wailsjs/runtime/runtime'
import { AppIcon } from '@/components/AppIcon'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useI18n, type MessageKey } from '@/i18n'
import { usePlatform } from '@/lib/platform'
import { formatBytes } from '@/lib/format'
import { cn } from 'cn'
import { ActionDialog } from './ActionDialog'

type Risk = 'safe' | 'review' | 'system' | 'never'
const RISKS: Risk[] = ['safe', 'review', 'system', 'never']

const riskStyle: Record<Risk, string> = {
  safe: 'border-emerald-600/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  review: 'border-amber-600/40 bg-amber-500/10 text-amber-700 dark:text-amber-400',
  system: 'border-sky-600/40 bg-sky-500/10 text-sky-700 dark:text-sky-400',
  never: 'border-red-600/40 bg-red-500/10 text-red-700 dark:text-red-400',
}

/** Findings that share a rule and owner, e.g. every node_modules folder. */
type Group = {
  key: string
  first: app.CulpritDTO
  items: app.CulpritDTO[]
  alloc: number
}

function groupItems(items: app.CulpritDTO[]): Group[] {
  const map = new Map<string, Group>()
  for (const it of items) {
    const key = `${it.ruleId}\u0000${it.owner}`
    let g = map.get(key)
    if (!g) {
      g = { key, first: it, items: [], alloc: 0 }
      map.set(key, g)
    }
    g.items.push(it)
    g.alloc += it.alloc
  }
  return [...map.values()].sort((a, b) => b.alloc - a.alloc)
}

const catKey = (c: string) => `cat_${c.replace(/-/g, '_')}` as MessageKey
const riskKey = (r: string) => `risk_${r}` as MessageKey
const riskHelpKey = (r: string) => `riskHelp_${r}` as MessageKey

/** "Who is using my disk": findings from the rule pack and installed apps. */
export function CulpritsView({ scanId, refresh }: { scanId: number; refresh: number }) {
  const { t, locale } = useI18n()
  const [data, setData] = useState<app.CulpritsDTO | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [risk, setRisk] = useState<Risk | 'all'>('all')

  useEffect(() => {
    let live = true
    Culprits()
      .then((d) => live && setData(d))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [scanId, refresh])

  const groups = useMemo(() => {
    if (!data) return []
    return groupItems(risk === 'all' ? data.items : data.items.filter((i) => i.risk === risk))
  }, [data, risk])

  const riskTotals = useMemo(() => {
    const m: Record<string, number> = {}
    for (const it of data?.items ?? []) m[it.risk] = (m[it.risk] ?? 0) + it.alloc
    return m
  }, [data])

  if (error) return <p className="text-sm text-destructive">{error}</p>
  if (!data) return null

  const percent = data.scanAlloc > 0 ? data.explained / data.scanAlloc : 0

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <p className="text-sm">
          {t('explainedSummary', {
            explained: formatBytes(data.explained, locale),
            total: formatBytes(data.scanAlloc, locale),
            percent: percent.toLocaleString(locale, { style: 'percent', maximumFractionDigits: 0 }),
          })}
        </p>
        <div className="h-2 w-full overflow-hidden rounded bg-muted" aria-hidden>
          <div className="h-full rounded bg-primary/70" style={{ width: `${(percent * 100).toFixed(1)}%` }} />
        </div>
        <p className="text-xs text-muted-foreground">{t('unexplainedHint')}</p>
        {data.warning && <p className="text-xs text-amber-600">{t('warning', { warning: data.warning })}</p>}
      </div>

      <div className="flex flex-wrap gap-2" role="group">
        <Button size="sm" variant={risk === 'all' ? 'default' : 'outline'} onClick={() => setRisk('all')}>
          {t('allRisks')}
        </Button>
        {RISKS.filter((r) => riskTotals[r] !== undefined).map((r) => (
          <Button
            key={r}
            size="sm"
            variant={risk === r ? 'default' : 'outline'}
            title={t(riskHelpKey(r))}
            onClick={() => setRisk(r)}
          >
            {t(riskKey(r))} · {formatBytes(riskTotals[r], locale)}
          </Button>
        ))}
      </div>

      {groups.length === 0 && <p className="text-sm text-muted-foreground">{t('noCulprits')}</p>}
      {groups.map((g) => (
        <GroupCard key={g.key} group={g} />
      ))}
      {data.omitted > 0 && <p className="text-xs text-muted-foreground">{t('omitted', { count: data.omitted })}</p>}
    </div>
  )
}

function GroupCard({ group }: { group: Group }) {
  const { t, locale, formatNumber } = useI18n()
  const { first, items } = group
  const [showAll, setShowAll] = useState(false)
  const visible = showAll ? items : items.slice(0, 3)
  const text = (m: Record<string, string>) => m[locale] ?? m.en ?? ''
  const unknown = items.every((i) => i.unreadable)

  return (
    <Card>
      <CardHeader className="gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <AppIcon owner={first.owner} />
          <CardTitle className="text-base">{first.owner}</CardTitle>
          <Badge variant="outline" className={cn(riskStyle[first.risk as Risk])} title={t(riskHelpKey(first.risk))}>
            {t(riskKey(first.risk))}
          </Badge>
          <span className="text-xs text-muted-foreground">{t(catKey(first.category))}</span>
          <span
            className="ml-auto font-medium tabular-nums"
            title={unknown ? t('unknownSizeTitle') : undefined}
          >
            {unknown ? t('unknownSize') : formatBytes(group.alloc, locale)}
          </span>
        </div>
        <p className="text-sm text-muted-foreground">{text(first.explain)}</p>
      </CardHeader>
      <CardContent className="space-y-3">
        {first.suggest?.map((s, i) => <SuggestionRow key={i} s={s} text={text(s.text)} />)}

        <div className="space-y-1">
          {items.length > 1 && (
            <p className="text-xs text-muted-foreground">{t('locations', { count: formatNumber(items.length) })}</p>
          )}
          {visible.map((it) => (
            <LocationRow key={it.path} item={it} />
          ))}
          {items.length > 3 && (
            <Button size="sm" variant="link" className="px-0" onClick={() => setShowAll(!showAll)}>
              {showAll ? t('showLess') : t('showAll', { count: formatNumber(items.length) })}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function LocationRow({ item }: { item: app.CulpritDTO }) {
  const { t, locale } = useI18n()
  const revealLabel = t(usePlatform() === 'darwin' ? 'revealMac' : 'reveal')
  const [open, setOpen] = useState(false)
  const [handled, setHandled] = useState<app.ActionResultDTO | null>(null)

  return (
    <div className="flex items-center gap-2 text-sm">
      <code className="min-w-0 flex-1 truncate text-xs" title={item.path}>
        {item.path}
      </code>
      {handled && (
        <Badge variant="secondary" className="shrink-0">
          {t('actedOn', { outcome: handled.outcome === 'done' ? `−${formatBytes(handled.freed, locale)}` : handled.outcome })}
        </Badge>
      )}
      <span className="shrink-0 tabular-nums text-muted-foreground">
        {item.unreadable ? t('unknownSize') : formatBytes(item.alloc, locale)}
      </span>
      {item.actions.length > 0 && (
        <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
          {t('actions')}
        </Button>
      )}
      <Button
        size="icon-sm"
        variant="ghost"
        title={revealLabel}
        aria-label={revealLabel}
        onClick={() => void Reveal(item.path)}
      >
        <FolderSearch />
      </Button>
      {item.actions.length > 0 && (
        <ActionDialog item={item} open={open} onOpenChange={setOpen} onDone={setHandled} />
      )}
    </div>
  )
}

function SuggestionRow({ s, text }: { s: attribute.Suggestion; text: string }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    if (!s.command) return
    try {
      if (await ClipboardSetText(s.command)) {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }
    } catch {
      // Windows can briefly refuse clipboard access; the user can click again.
    }
  }

  return (
    <div className="space-y-1.5 rounded-md border bg-muted/40 p-3 text-sm">
      <p>{text}</p>
      {(s.command || s.settings) && (
        <div className="flex flex-wrap items-center gap-2">
          {s.command && (
            <>
              <code className="rounded bg-background px-2 py-1 text-xs">{s.command}</code>
              <Button size="sm" variant="outline" title={t('copyTitle')} onClick={copy}>
                {copied ? <Check /> : <Copy />} {copied ? t('copied') : t('copy')}
              </Button>
            </>
          )}
          {s.settings && (
            <Button size="sm" variant="outline" onClick={() => void OpenSettings(s.settings!)}>
              <Settings /> {t('openSettings')}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
