/**
 * One entry in the console's boxed tab strip. `to` makes it a route link; a
 * tab without one is a local view switch handled by the page that owns it.
 */
export interface Tab {
  key: string
  label: string
  to?: string
}
