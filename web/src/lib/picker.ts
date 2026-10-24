/**
 * Borrowing SnFS files for product art.
 *
 * A product's icon, hero background, and previews are Drive references, not
 * uploads: the write endpoint stores a `CloudFileReference` and the catalog
 * renders it through `sk-media`. Choosing the file is SnFS's job — its
 * `/picker` page is embedded in a frame, so this app never handles file bytes.
 *
 * One thing differs from a same-site embed like Stargate's profile art: **the
 * file must be public.** The marketplace is read anonymously, so a private
 * file would render as a broken image for every visitor. The picker reports
 * the file's permission row, and a private file is refused with a message that
 * says what to change rather than silently storing it.
 *
 * The embed SDK is loaded on first use rather than bundled: a console that
 * never touches product art should not pay for it.
 */

import { request } from '@/lib/api/client'
import type { CloudFile } from '@/lib/api/types'
import { SNFS_ORIGIN } from './media'
import type { PickerFile } from './picker-types'

export type { PickerFile }

/**
 * The subset of the picker SDK this app calls.
 *
 * `code` is the cross-domain path. The picker is framed from another site, so
 * its `SameSite=Lax` session cookie is withheld and the frame cannot complete
 * a sign-in redirect — the identity host refuses to be framed. Instead this
 * app asks its own server for a single-use hand-off code bound to the picker's
 * client; the frame redeems it on its own origin for a bearer token it keeps
 * in memory. A code is single-use and lasts about a minute, and this app never
 * holds a token for the drive.
 */
interface PickerSdk {
  openSnfsPicker: (
    config: { frameUrl: string; dialogTitle?: string; width?: number; height?: number },
    request: {
      options: {
        view?: 'files' | 'folders' | 'any'
        multiple?: boolean
        mimeTypes?: string[]
        title?: string
        description?: string
        upload?: boolean
        theme?: 'auto' | 'dark' | 'light'
        locale?: 'en' | 'zh-CN'
      }
      token?: string
      /** A single-use hand-off code for the picker's client. */
      code?: string
    },
    handlers?: { onReady?: () => void; onSignedOut?: () => void; onError?: (m: string) => void },
  ) => Promise<PickerFile[]>
}

declare global {
  interface Window {
    openSnfsPicker?: PickerSdk['openSnfsPicker']
  }
}

/**
 * Where the drive lives. `VITE_SNFS_URL` points a dev or self-hosted console at
 * its own drive; the default is the shared deployment.
 */

export const PICKER_FRAME_URL = `${SNFS_ORIGIN}/picker`

/**
 * What the console answers when asked for a picker code.
 *
 * `frame_url` is the address the code was bound to. The server owns it rather
 * than this module: the issuer validates the redirect target when it issues
 * the code, so the frame has to be opened at exactly that address. A
 * build-time value here has no way to know what the server was configured
 * with, and a mismatch would fail at redemption rather than at issue.
 */
interface PickerCode {
  code: string
  expiresIn: number
  frameUrl: string
}

/**
 * Asks this console for a hand-off code bound to the picker's client.
 *
 * A failure resolves to null rather than throwing: the picker still works
 * when it authenticates by cookie, which is the case whenever the console and
 * the picker share a registrable domain. Reporting the failure as a missing
 * code keeps that deployment working instead of breaking it over a feature it
 * never enabled.
 */
async function requestPickerCode(): Promise<PickerCode | null> {
  try {
    return await request<PickerCode>('/picker/code', { method: 'POST' })
  } catch {
    return null
  }
}

let loading: Promise<PickerSdk> | null = null

/** Loads the picker script once; a failed load clears so a retry can work. */
function loadSdk(): Promise<PickerSdk> {
  if (window.openSnfsPicker) return Promise.resolve({ openSnfsPicker: window.openSnfsPicker })
  if (loading) return loading

  loading = new Promise<PickerSdk>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = `${SNFS_ORIGIN}/picker/snfs-picker.js`
    script.async = true
    script.onload = () => {
      if (window.openSnfsPicker) resolve({ openSnfsPicker: window.openSnfsPicker })
      else reject(new Error('The SnFS picker loaded without its entry point.'))
    }
    script.onerror = () => reject(new Error('The SnFS picker could not be loaded.'))
    document.head.append(script)
  }).catch((error: unknown) => {
    loading = null
    throw error
  })

  return loading
}

/** A file is public when its permission row says so; anything else is not. */
export function isPublic(file: PickerFile): boolean {
  return file.file.permission_status?.visibility === 'public'
}

function metadataOf(file: PickerFile): Record<string, unknown> {
  return (file.file.object?.meta ?? file.file.file_meta ?? {}) as Record<string, unknown>
}

/**
 * The reference DistributionCenter stores.
 *
 * It keeps identity and the immutable metadata the catalog needs, and no URL:
 * the marketplace is rendered by `sk-media`, which resolves a file id against
 * the drive itself. Storing a URL would pin it to one drive origin and go stale
 * when the deployment moves.
 */
export function toReference(file: PickerFile): CloudFile {
  const meta = metadataOf(file)
  const width = Number(meta.width ?? 0)
  const height = Number(meta.height ?? 0)
  const blurhash = typeof meta.blurhash === 'string' ? meta.blurhash : ''
  return {
    id: file.id,
    name: file.name,
    mime_type: file.mime_type,
    size: file.size,
    ...(width > 0 && height > 0 ? { width, height } : {}),
    ...(blurhash ? { blurhash } : {}),
    file_meta: {},
    user_meta: {},
  }
}

export interface PickArtworkOptions {
  title: string
  description?: string
  /** More than one, for the preview strip. */
  multiple?: boolean
  width?: number
  height?: number
}

export type PickOutcome =
  | { kind: 'picked'; references: CloudFile[] }
  | { kind: 'cancelled' }
  | { kind: 'private'; names: string[] }

/**
 * Opens the drive picker for images and resolves with what was chosen.
 *
 * A private file is reported rather than stored: the marketplace renders art
 * for signed-out visitors, and a reference they cannot load is a broken image
 * with no explanation. The person can share the file in the drive and pick it
 * again.
 */
export async function pickArtwork(options: PickArtworkOptions): Promise<PickOutcome> {
  const [sdk, grant] = await Promise.all([loadSdk(), requestPickerCode()])

  // The server names the frame when it issues a code, because that is the
  // address the code is bound to. A deployment without the picker configured
  // answers no code, and the frame falls back to the build-time URL, which is
  // the same-site case the cookie already covers.
  const files = await sdk.openSnfsPicker(
    {
      frameUrl: grant?.frameUrl || PICKER_FRAME_URL,
      dialogTitle: options.title,
      width: options.width ?? 760,
      height: options.height ?? 580,
    },
    {
      options: {
        view: 'files',
        multiple: options.multiple ?? false,
        mimeTypes: ['image/*'],
        upload: true,
        title: options.title,
        description: options.description,
        theme: document.documentElement.dataset.theme === 'express-light' ? 'light' : 'dark',
        locale: document.documentElement.lang === 'zh-CN' ? 'zh-CN' : 'en',
      },
      // A code crosses the boundary instead of a token: it is single-use, it
      // lasts about a minute, and only the picker's own server holds the
      // secret that redeems it. This app never sees a token for the drive.
      ...(grant ? { code: grant.code } : {}),
    },
  )
  if (!files.length) return { kind: 'cancelled' }

  const hidden = files.filter((file) => !isPublic(file))
  if (hidden.length) return { kind: 'private', names: hidden.map((file) => file.name) }

  return { kind: 'picked', references: files.map(toReference) }
}
