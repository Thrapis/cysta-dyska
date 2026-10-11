import { useEffect, useState } from 'react'
import { Settings as SettingsIcon } from 'lucide-react'
import { SetAllowPermanentDelete, Settings } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { useI18n } from '@/i18n'

/** Header button opening the settings dialog. */
export function SettingsButton({ onChange }: { onChange: () => void }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [settings, setSettings] = useState<app.SettingsDTO | null>(null)
  const [error, setError] = useState<string | null>(null)

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

  return (
    <>
      <Button size="icon" variant="ghost" title={t('settings')} aria-label={t('settings')} onClick={() => setOpen(true)}>
        <SettingsIcon />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings')}</DialogTitle>
          </DialogHeader>
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
        </DialogContent>
      </Dialog>
    </>
  )
}
