/**
 * The wire shape of a picked file, as `snfs-picker.js` sends it.
 *
 * DistributionCenter only stores a Drive reference and resolves the rest in
 * the catalog, so this is the subset the console reads: identity, the metadata
 * a `CloudFileReference` keeps locally, and the permission row that says
 * whether an anonymous visitor will be able to load the picture at all.
 *
 * It is redeclared rather than imported because the picker ships as a plain
 * script from another origin — there is no module to import at build time, and
 * a type-only import from `DysonFS/web` would couple the two repositories.
 */
export interface PickerFile {
  id: string
  name: string
  is_folder: boolean
  mime_type: string
  size: number
  extension: string
  kind: string
  parent_id: string | null
  created_at: string
  updated_at: string
  /** Same-origin with the drive; carries the drive's session cookie. */
  url: string
  thumbnail_url: string | null
  /** The API row, for the fields this shape does not name. */
  file: {
    file_meta?: Record<string, unknown> | null
    object?: { meta?: Record<string, unknown> | null } | null
    permission_status?: { visibility?: string; readable?: boolean } | null
  }
}
