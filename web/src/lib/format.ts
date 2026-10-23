import { currentLocale, translate } from '@/lib/i18n'
import type { LocalizedText } from '@/lib/api/types'

const minute = 60_000
const hour = 60 * minute
const daySpan = 24 * hour

/** Absolute UTC stamp in the format the server logs use. */
export function utcStamp(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${date.toISOString().slice(0, 19).replace('T', ' ')}Z`
}

/** Calendar date only: 2026-10-23. */
export function day(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toISOString().slice(0, 10)
}

/** Compact age: 42s, 7m, 3h, 12d. */
export function age(value?: string | null): string {
  if (!value) return '—'
  const ms = Date.now() - new Date(value).getTime()
  if (Number.isNaN(ms)) return '—'
  if (ms < 0) return translate('time.pending')
  if (ms < minute) return `${Math.max(1, Math.floor(ms / 1000))}s`
  if (ms < hour) return `${Math.floor(ms / minute)}m`
  if (ms < daySpan) return `${Math.floor(ms / hour)}h`
  return `${Math.floor(ms / daySpan)}d`
}

export function relative(value?: string | null): string {
  if (!value) return '—'
  const ms = Date.now() - new Date(value).getTime()
  if (Number.isNaN(ms)) return '—'
  if (ms < minute) return translate('time.justNow')
  if (ms < hour) return translate('time.minutesAgo', { n: Math.floor(ms / minute) })
  if (ms < daySpan) return translate('time.hoursAgo', { n: Math.floor(ms / hour) })
  if (ms < 30 * daySpan) return translate('time.daysAgo', { n: Math.floor(ms / daySpan) })
  return utcStamp(value).slice(0, 10)
}

export function bytes(value?: number): string {
  if (!value) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

export function compactNumber(value?: number): string {
  if (value === undefined || value === null) return '—'
  return new Intl.NumberFormat(currentLocale(), {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)
}

export function count(value?: number): string {
  if (value === undefined || value === null) return '—'
  return new Intl.NumberFormat(currentLocale()).format(value)
}

/** Short id for dense ledgers; full value stays available via title. */
export function shortId(value?: string | null): string {
  if (!value) return '—'
  return value.length > 8 ? value.slice(0, 8) : value
}

/**
 * Picks the reader's translation from a publisher's localized map. The
 * default text is the fallback, and an unknown locale falls back to the map's
 * first entry rather than showing nothing.
 */
export function localized(
  text: LocalizedText | undefined,
  fallback: string | undefined,
  locale = currentLocale(),
): string {
  if (!text) return fallback ?? ''
  const language = locale.toLowerCase()
  const exact = text[locale]
  if (exact) return exact
  const base = language.split('-')[0]
  for (const [key, value] of Object.entries(text)) {
    if (key.toLowerCase() === language) return value
    if (base && key.toLowerCase().split('-')[0] === base) return value
  }
  return fallback || Object.values(text)[0] || ''
}

/** A platform and architecture joined as one artifact target. */
export function target(platform?: string, architecture?: string): string {
  if (!platform && !architecture) return '—'
  return `${platform || 'any'}/${architecture || 'any'}`
}

/** Splits a comma/space separated list into trimmed tokens. */
export function tokens(value: string): string[] {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}
