import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { be } from './be'
import { en, type Messages } from './en'

export const locales = { en, be } as const
export type Locale = keyof typeof locales
export type MessageKey = keyof Messages

const STORAGE_KEY = 'cysta-dyska.locale'

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && saved in locales) return saved as Locale
  } catch {
    // storage unavailable: fall through to the browser language
  }
  return navigator.language.toLowerCase().startsWith('be') ? 'be' : 'en'
}

type I18n = {
  locale: Locale
  setLocale: (l: Locale) => void
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
  formatNumber: (n: number) => string
}

const I18nContext = createContext<I18n | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale)

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l)
    try {
      localStorage.setItem(STORAGE_KEY, l)
    } catch {
      // not persisted; the choice still applies for this session
    }
  }, [])

  const value = useMemo<I18n>(() => {
    const nf = new Intl.NumberFormat(locale)
    return {
      locale,
      setLocale,
      t: (key, vars) =>
        locales[locale][key].replace(/\{(\w+)\}/g, (m, name: string) =>
          vars && name in vars ? String(vars[name]) : m,
        ),
      formatNumber: (n) => nf.format(n),
    }
  }, [locale, setLocale])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18n {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used inside I18nProvider')
  return ctx
}
