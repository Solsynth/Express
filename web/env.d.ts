/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Base URL of the DistributionCenter API in development. */
  readonly VITE_API_TARGET?: string
  /** Sign-in page of the identity host, for the shared-session hand-off. */
  readonly VITE_AUTH_URL?: string
  /** Origin of the drive's picker page. */
  readonly VITE_SNFS_URL?: string
  /** Origin that resolves drive file ids for art (default api.solian.app). */
  readonly VITE_SNFS_API_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
