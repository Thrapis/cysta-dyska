import { useState } from 'react'
import { FolderOpen, HardDrive, Loader2, ShieldCheck, Square, ShieldAlert } from 'lucide-react'
import { PickFolder } from '../wailsjs/go/main/Shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CulpritsView } from '@/features/culprits/CulpritsView'
import { HistoryView } from '@/features/history/HistoryView'
import { TreemapView } from '@/features/map/TreemapView'
import { ActivityView } from '@/features/activity/ActivityView'
import { SettingsButton } from '@/features/settings/SettingsButton'
import { SizeTree } from '@/features/scan/SizeTree'
import { useScan, type ScanState } from '@/features/scan/useScan'
import { useI18n } from '@/i18n'
import { formatBytes, formatElapsed } from '@/lib/format'

export default function App() {
  const { t } = useI18n()
  const { state, start, cancel } = useScan()
  const [folder, setFolder] = useState('')
  // Bumped when settings change, so views that depend on them reload.
  const [settingsVersion, setSettingsVersion] = useState(0)
  const scanning = state.status === 'scanning'
  // The full scan reads a whole drive's file table, so it is offered for drive roots only.
  const isDrive = /^[A-Za-z]:\\?$/.test(folder)

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
        <SettingsButton onChange={() => setSettingsVersion((v) => v + 1)} />
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
          <>
            {isDrive && (
              <Button variant="outline" onClick={() => start(folder, true)} title={t('fullScanHint')}>
                <ShieldAlert /> {t('fullScan')}
              </Button>
            )}
            <Button onClick={() => start(folder)} disabled={!folder}>
              {state.status === 'done' ? t('rescan') : t('scan')}
            </Button>
          </>
        )}
      </div>

      <main className="min-h-0 flex-1 overflow-auto px-5 py-4">
        <Status state={state} />
        {state.status === 'done' && (
          <Tabs defaultValue="culprits">
            <TabsList>
              <TabsTrigger value="culprits">{t('tabCulprits')}</TabsTrigger>
              <TabsTrigger value="folders">{t('tabFolders')}</TabsTrigger>
              <TabsTrigger value="map">{t('tabMap')}</TabsTrigger>
              <TabsTrigger value="history">{t('tabHistory')}</TabsTrigger>
              <TabsTrigger value="activity">{t('tabActivity')}</TabsTrigger>
            </TabsList>
            <TabsContent value="culprits" className="pt-3">
              <ErrorBoundary key={state.scanId}>
                <CulpritsView scanId={state.scanId} refresh={settingsVersion} />
              </ErrorBoundary>
            </TabsContent>
            <TabsContent value="folders" className="pt-3">
              <ErrorBoundary key={state.scanId}>
                <SizeTree root={state.root} scanId={state.scanId} />
              </ErrorBoundary>
            </TabsContent>
            <TabsContent value="map" className="pt-3">
              <ErrorBoundary key={state.scanId}>
                <TreemapView key={state.scanId} root={state.root} scanId={state.scanId} />
              </ErrorBoundary>
            </TabsContent>
            <TabsContent value="history" className="pt-3">
              <ErrorBoundary key={state.scanId}>
                <HistoryView scanId={state.scanId} />
              </ErrorBoundary>
            </TabsContent>
            <TabsContent value="activity" className="pt-3">
              <ErrorBoundary key={state.scanId}>
                <ActivityView refresh={settingsVersion} />
              </ErrorBoundary>
            </TabsContent>
          </Tabs>
        )}
      </main>

      <footer className="flex flex-wrap items-center gap-2 border-t px-5 py-2 text-xs text-muted-foreground">
        <Badge variant="secondary">
          <ShieldCheck /> {t('safetyNote')}
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
              <Loader2 className="size-4 animate-spin" /> {t(state.fast ? 'fullScanning' : 'scanning')}
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
    case 'declined':
      return <p className="mb-3 text-sm text-muted-foreground">{t('fullScanDeclined')}</p>
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
