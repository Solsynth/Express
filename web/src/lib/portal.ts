/**
 * The developer portal's vocabulary.
 *
 * The portal's enums are asymmetric across the wire. A **stored** record
 * serializes its enum as a number — `CustomApp.status`, `AppProduct.recurrence`,
 * `AppProductState.stock_mode`, and `MiniApp.stage` are Go `int` types with no
 * `MarshalJSON`. The **input** struct the API accepts takes a word instead:
 * `CustomAppInput.status` is a `*string` fed to `ParseCustomAppStatus`, and
 * `ProductInput.recurrence` likewise. A write that sent the ordinal back would
 * be rejected with `cannot unmarshal number into … of type string`.
 *
 * So a read needs a number-to-word mapping and a write sends the word. The
 * word lists below are the console's vocabulary; the functions decode a read.
 */

/** Custom app status. */
export const APP_STATUSES = ['developing', 'staging', 'production', 'suspended'] as const
export type AppStatus = (typeof APP_STATUSES)[number]

/** App product recurrence. */
export const RECURRENCES = ['none', 'weekly', 'monthly', 'yearly'] as const
export type Recurrence = (typeof RECURRENCES)[number]

/** App product stock mode. */
export const STOCK_MODES = [
  'unlimited',
  'daily',
  'weekly',
  'monthly',
  'yearly',
  'manual',
] as const
export type StockMode = (typeof STOCK_MODES)[number]

/** Mini app stage. */
export const MINI_APP_STAGES = ['development', 'staging', 'production'] as const
export type MiniAppStage = (typeof MINI_APP_STAGES)[number]

/** Board widget field types. */
export const BOARD_FIELD_TYPES = [
  'string',
  'number',
  'boolean',
  'object',
  'array',
  'null',
] as const
export type BoardFieldTypeName = (typeof BOARD_FIELD_TYPES)[number]

/** The API word for a numeric app status. */
export function appStatus(value: number): AppStatus {
  return APP_STATUSES[value] ?? 'developing'
}

/** The API word for a numeric recurrence. */
export function recurrence(value: number): Recurrence {
  return RECURRENCES[value] ?? 'none'
}

/** The API word for a numeric stock mode. */
export function stockMode(value: number): StockMode {
  return STOCK_MODES[value] ?? 'unlimited'
}

/** The API word for a numeric mini app stage. */
export function miniAppStage(value: number): MiniAppStage {
  return MINI_APP_STAGES[value] ?? 'development'
}

/**
 * The expiry a secret carries, as a Go duration string.
 *
 * The API takes a duration such as `24h`, not a timestamp: the server owns
 * the clock, and a client-computed instant would drift from it.
 */
export function expiryDuration(hours: number): string {
  if (hours <= 0) return ''
  return `${hours}h`
}
