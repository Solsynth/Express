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

import type { CloudFile } from '@/lib/api/types'
import { SNFS_ORIGIN } from './media'
import type { PickerFile } from './picker-types'

export type { PickerFile }

/** The subset of the picker SDK this app calls. */
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
  const sdk = await loadSdk()
  const files = await sdk.openSnfsPicker(
    {
      frameUrl: PICKER_FRAME_URL,
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
    },
  )
  if (!files.length) return { kind: 'cancelled' }

  const hidden = files.filter((file) => !isPublic(file))
  if (hidden.length) return { kind: 'private', names: hidden.map((file) => file.name) }

  return { kind: 'picked', references: files.map(toReference) }
}
