import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/i18n'

type Props = { children: ReactNode }
type State = { error: Error | null }

/**
 * Catches a render error in one part of the UI and shows a message with a
 * retry button instead of unmounting the whole app into a black window.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return <Fallback error={this.state.error} onRetry={() => this.setState({ error: null })} />
    }
    return this.props.children
  }
}

function Fallback({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const { t } = useI18n()
  return (
    <div role="alert" className="space-y-2 rounded-md border border-destructive/40 p-4 text-sm">
      <p className="font-medium text-destructive">{t('crashTitle')}</p>
      <p className="text-muted-foreground">{t('crashBody')}</p>
      <pre className="max-h-24 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">{error.message}</pre>
      <Button size="sm" variant="outline" onClick={onRetry}>
        {t('crashRetry')}
      </Button>
    </div>
  )
}
