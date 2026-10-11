import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { PlanAction, RunAction } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useI18n, type MessageKey } from '@/i18n'
import { formatBytes } from '@/lib/format'

type Step =
  | { name: 'choose' }
  | { name: 'planning'; option: app.ActionOptionDTO }
  | { name: 'confirm'; option: app.ActionOptionDTO; plan: app.PlanDTO }
  | { name: 'running'; plan: app.PlanDTO }
  | { name: 'result'; result: app.ActionResultDTO; kind: string }
  | { name: 'error'; message: string }

const kindKey = (k: string) => `kind_${k}` as MessageKey
const resultKey = (outcome: string, kind: string) =>
  (outcome === 'done' && kind === 'recycle' ? 'outcome_done_recycle' : `outcome_${outcome}`) as MessageKey
const kindHelpKey = (k: string) => `kindHelp_${k}` as MessageKey
const confirmKey: Record<string, MessageKey> = {
  command: 'confirmRun',
  admin: 'confirmRun',
  recycle: 'confirmRecycle',
  delete: 'confirmDelete',
  uninstall: 'confirmUninstall',
}

/** Choose an action for one finding, see exactly what it will do, confirm, and see the result. */
export function ActionDialog({
  item,
  open,
  onOpenChange,
  onDone,
}: {
  item: app.CulpritDTO
  open: boolean
  onOpenChange: (open: boolean) => void
  onDone: (result: app.ActionResultDTO) => void
}) {
  const { t, locale } = useI18n()
  const [step, setStep] = useState<Step>({ name: 'choose' })
  const [ack, setAck] = useState(false)
  const name = item.path.slice(item.path.lastIndexOf('\\') + 1) || item.path
  const busy = step.name === 'planning' || step.name === 'running'

  const reset = () => {
    setStep({ name: 'choose' })
    setAck(false)
  }

  const plan = async (option: app.ActionOptionDTO) => {
    setStep({ name: 'planning', option })
    try {
      const p = await PlanAction({ ruleId: item.ruleId, path: item.path, kind: option.kind, suggest: option.suggest })
      setStep({ name: 'confirm', option, plan: p })
    } catch (e) {
      setStep({ name: 'error', message: String(e) })
    }
  }

  const run = async (p: app.PlanDTO) => {
    setStep({ name: 'running', plan: p })
    try {
      const result = await RunAction(p.id, ack)
      setStep({ name: 'result', result, kind: p.kind })
      onDone(result)
    } catch (e) {
      setStep({ name: 'error', message: String(e) })
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (busy) return // never close while measuring or running
        if (!o) reset()
        onOpenChange(o)
      }}
    >
      <DialogContent showCloseButton={!busy} className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('actionsTitle', { name })}</DialogTitle>
          <DialogDescription className="break-all font-mono text-xs">{item.path}</DialogDescription>
        </DialogHeader>

        {step.name === 'choose' && (
          <div className="space-y-2">
            {item.actions.length === 0 && <p className="text-sm text-muted-foreground">{t('noActions')}</p>}
            {item.actions.map((o) => (
              <button
                key={`${o.kind}-${o.suggest}`}
                type="button"
                disabled={!!o.blocked}
                onClick={() => plan(o)}
                className="w-full rounded-md border p-3 text-left hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60"
              >
                <span className="block font-medium">{t(kindKey(o.kind))}</span>
                {o.command && <code className="mt-1 block truncate text-xs">{o.command}</code>}
                <span className="mt-1 block text-xs text-muted-foreground">
                  {o.blocked ? t('planBlocked', { reason: o.blocked }) : t(kindHelpKey(o.kind))}
                </span>
              </button>
            ))}
          </div>
        )}

        {step.name === 'planning' && (
          <p className="flex items-center gap-2 text-sm">
            <Loader2 className="size-4 animate-spin" /> {t('measuring')}
          </p>
        )}

        {step.name === 'confirm' && (
          <PlanDetails plan={step.plan} ack={ack} onAck={setAck} />
        )}

        {step.name === 'running' && (
          <p className="flex items-center gap-2 text-sm">
            <Loader2 className="size-4 animate-spin" /> {t('running')}
          </p>
        )}

        {step.name === 'result' && (
          <div className="space-y-2 text-sm">
            <p className={step.result.outcome === 'failed' ? 'text-destructive' : ''}>
              {t(resultKey(step.result.outcome, step.kind), {
                size: formatBytes(step.result.freed, locale),
                detail: step.result.detail,
              })}
            </p>
            {step.result.output && (
              <details>
                <summary className="cursor-pointer text-xs text-muted-foreground">{t('showOutput')}</summary>
                <pre className="mt-1 max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                  {step.result.output}
                </pre>
              </details>
            )}
            <p className="text-xs text-muted-foreground">{t('rescanHint')}</p>
          </div>
        )}

        {step.name === 'error' && <p className="text-sm text-destructive">{step.message}</p>}

        <DialogFooter>
          {(step.name === 'confirm' || step.name === 'error') && (
            <Button variant="outline" onClick={reset}>
              {t('back')}
            </Button>
          )}
          {step.name === 'confirm' && (
            <Button
              variant={step.plan.kind === 'delete' ? 'destructive' : 'default'}
              disabled={!!step.plan.blocked || (step.plan.needsAck && !ack)}
              onClick={() => run(step.plan)}
            >
              {t(confirmKey[step.plan.kind] ?? 'confirmRun')}
              {(step.plan.kind === 'recycle' || step.plan.kind === 'delete') && ` · ${formatBytes(step.plan.bytes, locale)}`}
            </Button>
          )}
          {step.name === 'result' && (
            <Button
              onClick={() => {
                reset()
                onOpenChange(false)
              }}
            >
              {t('close')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PlanDetails({ plan, ack, onAck }: { plan: app.PlanDTO; ack: boolean; onAck: (v: boolean) => void }) {
  const { t, locale, formatNumber } = useI18n()
  const fileAction = plan.kind === 'recycle' || plan.kind === 'delete'

  return (
    <div className="space-y-3 text-sm">
      {fileAction ? (
        <>
          <p className={plan.kind === 'delete' ? 'font-medium text-destructive' : 'font-medium'}>
            {t(plan.kind === 'delete' ? 'planWillDelete' : 'planWillMove', { count: formatNumber(plan.targetCount) })}
          </p>
          <ul className="max-h-40 space-y-0.5 overflow-auto rounded border bg-muted/40 p-2 font-mono text-xs">
            {plan.targets.map((p) => (
              <li key={p} className="truncate" title={p}>
                {p}
              </li>
            ))}
            {plan.targetCount > plan.targets.length && (
              <li className="text-muted-foreground">
                {t('planAndMore', { count: formatNumber(plan.targetCount - plan.targets.length) })}
              </li>
            )}
          </ul>
          <p>{t('planSize', { size: formatBytes(plan.bytes, locale), files: formatNumber(plan.files) })}</p>
        </>
      ) : (
        <>
          <p className="font-medium">{t('planWillRun')}</p>
          <code className="block rounded bg-muted p-2 text-xs break-all">{plan.display}</code>
        </>
      )}
      <p className="text-xs text-muted-foreground">{t(`kindHelp_${plan.kind}` as MessageKey)}</p>
      {plan.warnings.map((w) => (
        <p key={w} className="text-xs text-amber-600">
          {w}
        </p>
      ))}
      {plan.blocked && <p className="text-destructive">{t('planBlocked', { reason: plan.blocked })}</p>}
      {plan.needsAck && !plan.blocked && (
        <label className="flex items-start gap-2">
          <Checkbox checked={ack} onCheckedChange={(v) => onAck(v === true)} className="mt-0.5" />
          <span>{t('planAck')}</span>
        </label>
      )}
    </div>
  )
}
