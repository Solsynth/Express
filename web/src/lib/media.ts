/**
 * SunkenLand's elements, configured for this console.
 *
 * `sk-media` renders a drive file — image, video, audio, or a file card — from
 * a bare file id, resolving metadata through the drive's public info endpoint.
 * Express uses it for product art — icon, hero, previews — which are Drive
 * references rather than uploads, so the same widget that draws a post
 * attachment draws a marketplace icon.
 *
 * The package registers its tags on import; importing it lazily would register
 * nothing, so this module is imported by the shell once and its elements are
 * usable everywhere afterwards.
 *
 * The preset is passed as a URL rather than imported as CSS: the element
 * injects it as a `<link>` into its shadow root, which is the only way styles
 * reach shadow DOM from outside. Vite resolves the package path to a hashed
 * asset.
 */

import mediaPreset from '@solsynth/sunkenland/presets/media.css?url'
import { configure, defineMedia } from '@solsynth/sunkenland'

/**
 * The drive whose file ids these references point at.
 *
 * `api.solian.app` is the fleet API host that fronts the drive; a self-hosted
 * deployment points this at its own drive origin, which must serve
 * `/drive/files/{id}` for anonymous reads.
 */
export const DRIVE_API = (import.meta.env.VITE_SNFS_API_URL || 'https://api.solian.app').replace(
  /\/+$/,
  '',
)

/**
 * Where the picker page lives. The picker and the file reads are the same
 * service but not always the same host: the fleet serves the page at
 * `fs.solian.app` and the API at `api.solian.app`, so this is configured
 * separately rather than derived from `DRIVE_API`.
 */
export const SNFS_ORIGIN = (import.meta.env.VITE_SNFS_URL || 'https://fs.solian.app').replace(
  /\/+$/,
  '',
)

let ready = false

/**
 * Configures the element library once. Safe to call from several places: the
 * configuration is page-wide, so a second call is a no-op rather than a
 * re-registration (which the custom-element registry would reject).
 */
export function installMediaElements(): void {
  if (ready) return
  ready = true
  defineMedia()
  // `baseUrl` is what the element resolves `/drive/files/{id}` against; without
  // it the library falls back to the fleet default even when this deployment
  // points at another drive.
  configure({ baseUrl: DRIVE_API, css: mediaPreset })
}
