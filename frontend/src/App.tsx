import { useState } from 'react'
import { FolderOpen, HardDrive, Loader2, ShieldCheck, Square } from 'lucide-react'
import { PickFolder } from '../wailsjs/go/main/Shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CulpritsView } from '@/features/culprits/CulpritsView'
import { SizeTree } from '@/features/scan/SizeTree'
import { useScan, type ScanState } from '@/features/scan/useScan'
import { locales, useI18n, type Locale } from '@/i18n'
import { formatBytes, formatElapsed } from '@/lib/format'

export default function App() {
  const { t, locale, setLocale } = useI18n()
  const { state, start, cancel } = useScan()
  const [folder, setFolder] = useState('')
  const scanning = state.status === 'scanning'

  const pick = async () => {
    const path = await PickFolder()
    if (path) setFolder(path)
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <header className="flex items-center gap-3 border-b px-5 py-3">
        <HardDrive className="size-6 text-primary" />
        <div className="min-w-0 flex-1">
          <h1 className="font-heading text-lg font-semibold leading-tight">{t('appName')}</h1>
          <p className="text-sm text-muted-foreground">{t('tagline')}</p>
        </div>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          {t('language')}
          <select
            className="rounded-md border bg-background px-2 py-1 text-foreground"
            value={locale}
            onChange={(e) => setLocale(e.target.value as Locale)}
          >
            {(Object.keys(locales) as Locale[]).map((l) => (
              <option key={l} value={l}>
                {l.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
      </header>

      <div className="flex flex-wrap items-center gap-2 border-b px-5 py-3">
        <Button variant="outline" onClick={pick} disabled={scanning}>
          <FolderOpen /> {t('chooseFolder')}
        </Button>
        <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1.5 text-sm" title={folder}>
          {folder || t('noFolder')}
        </code>
        {scanning ? (
          <Button variant="destructive" onClick={cancel}>
            <Square /> {t('cancel')}
          </Button>
        ) : (
          <Button onClick={() => start(folder)} disabled={!folder}>
            {state.status === 'done' ? t('rescan') : t('scan')}
          </Button>
        )}
      </div>

      <main className="min-h-0 flex-1 overflow-auto px-5 py-4">
        <Status state={state} />
        {state.status === 'done' && (
          <Tabs defaultValue="culprits">
            <TabsList>
              <TabsTrigger value="culprits">{t('tabCulprits')}</TabsTrigger>
              <TabsTrigger value="folders">{t('tabFolders')}</TabsTrigger>
            </TabsList>
            <TabsContent value="culprits" className="pt-3">
              <CulpritsView scanId={state.scanId} />
            </TabsContent>
            <TabsContent value="folders" className="pt-3">
              <SizeTree root={state.root} scanId={state.scanId} />
            </TabsContent>
          </Tabs>
        )}
      </main>

      <footer className="flex flex-wrap items-center gap-2 border-t px-5 py-2 text-xs text-muted-foreground">
        <Badge variant="secondary">
          <ShieldCheck /> {t('readOnlyNote')}
        </Badge>
        <span>{t('sizeNote')}</span>
      </footer>
    </div>
  )
}

function Status({ state }: { state: ScanState }) {
  const { t, locale, formatNumber } = useI18n()

  switch (state.status) {
    case 'idle':
      return null
    case 'scanning': {
      const p = state.progress
      return (
        <Card className="mb-4">
          <CardContent className="space-y-2">
            <div className="flex items-center gap-2 font-medium">
              <Loader2 className="size-4 animate-spin" /> {t('scanning')}
            </div>
            {p && (
              <>
                <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm tabular-nums">
                  <Stat label={t('size')} value={formatBytes(p.bytes, locale)} />
                  <Stat label={t('files')} value={formatNumber(p.files)} />
                  <Stat label={t('folders')} value={formatNumber(p.dirs)} />
                  <Stat label={t('elapsed')} value={formatElapsed(p.elapsedMs)} />
                  {p.errors > 0 && <Stat label={t('unreadable')} value={formatNumber(p.errors)} />}
                </div>
                <p className="truncate text-xs text-muted-foreground" title={p.path}>
                  {p.path}
                </p>
              </>
            )}
          </CardContent>
        </Card>
      )
    }
    case 'done':
      return (
        <p className="mb-3 text-sm text-muted-foreground">
          {t('scanDone', {
            size: formatBytes(state.root.alloc, locale),
            files: formatNumber(state.root.files),
            dirs: formatNumber(state.root.dirs),
          })}
        </p>
      )
    case 'cancelled':
      return <p className="mb-3 text-sm text-muted-foreground">{t('cancelled')}</p>
    case 'error':
      return <p className="mb-3 text-sm text-destructive">{t('scanFailed', { error: state.error })}</p>
  }
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <span>
      <span className="text-muted-foreground">{label}: </span>
      {value}
    </span>
  )
}
