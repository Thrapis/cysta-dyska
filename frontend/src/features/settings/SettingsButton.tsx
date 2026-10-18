import { useEffect, useState } from 'react'
import { Loader2, Settings as SettingsIcon } from 'lucide-react'
import { ClearData, SetAllowPermanentDelete, Settings } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { locales, useI18n, type Locale } from '@/i18n'

// Each language is named in itself, so it stays findable whatever the UI language is.
const localeNames: Record<Locale, string> = { en: 'English', be: 'Беларуская' }

type ClearStep = 'idle' | 'confirm' | 'clearing' | 'done'

/** Header button opening the settings dialog. */
export function SettingsButton({ onChange }: { onChange: () => void }) {
  const { t, locale, setLocale } = useI18n()
  const [open, setOpen] = useState(false)
  const [settings, setSettings] = useState<app.SettingsDTO | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [clearStep, setClearStep] = useState<ClearStep>('idle')

  useEffect(() => {
    if (!open) return
    let live = true
    Settings()
      .then((s) => live && setSettings(s))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [open])

  const toggle = async (on: boolean) => {
    try {
      await SetAllowPermanentDelete(on)
      setSettings((s) => (s ? { ...s, allowPermanentDelete: on } : s))
      onChange()
    } catch (e) {
      setError(String(e))
    }
  }

  const clear = async () => {
    setError(null)
    setClearStep('clearing')
    try {
      await ClearData()
      setClearStep('done')
      onChange()
    } catch (e) {
      setError(String(e))
      setClearStep('idle')
    }
  }

  const openDialog = () => {
    setClearStep('idle')
    setError(null)
    setOpen(true)
  }

  const busy = clearStep === 'clearing'

  return (
    <>
      <Button size="icon" variant="ghost" title={t('settings')} aria-label={t('settings')} onClick={openDialog}>
        <SettingsIcon />
      </Button>
      <Dialog open={open} onOpenChange={(o) => !busy && setOpen(o)}>
        <DialogContent showCloseButton={!busy}>
          <DialogHeader>
            <DialogTitle>{t('settings')}</DialogTitle>
          </DialogHeader>
          <label className="flex items-center justify-between gap-4 text-sm font-medium">
            {t('language')}
            <select
              className="rounded-md border bg-background px-2 py-1 font-normal text-foreground"
              value={locale}
              onChange={(e) => setLocale(e.target.value as Locale)}
            >
              {(Object.keys(locales) as Locale[]).map((l) => (
                <option key={l} value={l}>
                  {localeNames[l]}
                </option>
              ))}
            </select>
          </label>
          {error && <p className="text-sm text-destructive">{error}</p>}
          {settings && !settings.available && (
            <p className="text-sm text-muted-foreground">{t('settingsUnavailable')}</p>
          )}
          {settings?.available && (
            <label className="flex items-start justify-between gap-4">
              <span className="space-y-1">
                <span className="block text-sm font-medium">{t('settingDelete')}</span>
                <span className="block text-xs text-muted-foreground">{t('settingDeleteHelp')}</span>
              </span>
              <Switch checked={settings.allowPermanentDelete} onCheckedChange={toggle} />
            </label>
          )}
          <div className="space-y-2 border-t pt-4">
            <div className="text-sm font-medium">{t('settingClear')}</div>
            <p className="text-xs text-muted-foreground">{t('settingClearHelp')}</p>
            {clearStep === 'idle' && (
              <Button variant="outline" onClick={() => setClearStep('confirm')}>
                {t('settingClear')}
              </Button>
            )}
            {(clearStep === 'confirm' || clearStep === 'clearing') && (
              <div className="space-y-2">
                <p className="text-sm">{t('clearConfirm')}</p>
                <div className="flex gap-2">
                  <Button variant="outline" disabled={busy} onClick={() => setClearStep('idle')}>
                    {t('cancel')}
                  </Button>
                  <Button variant="destructive" disabled={busy} onClick={clear}>
                    {busy ? (
                      <>
                        <Loader2 className="animate-spin" /> {t('clearing')}
                      </>
                    ) : (
                      t('settingClear')
                    )}
                  </Button>
                </div>
              </div>
            )}
            {clearStep === 'done' && <p className="text-sm text-muted-foreground">{t('clearDone')}</p>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
