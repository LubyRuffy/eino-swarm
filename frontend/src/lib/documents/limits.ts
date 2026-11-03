/** Preview bounds. The download cap is still 8 MiB; these stop a zip or a
 * sheet from expanding into an unbounded document after that. */
export const MAX_ZIP_ENTRIES = 4096
export const MAX_ZIP_ENTRY_BYTES = 16 * 1024 * 1024
export const MAX_ZIP_TOTAL_BYTES = 32 * 1024 * 1024
export const MAX_PREVIEW_CELLS = 50_000
export const MAX_HTML_ASSETS = 64
export const MAX_HTML_ASSET_BYTES = 4 * 1024 * 1024
export const MAX_SHEET_ROWS = 5_000
export const MAX_SHEET_COLS = 100
