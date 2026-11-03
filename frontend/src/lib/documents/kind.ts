export type DocumentFormat = "docx" | "xlsx" | "pptx" | "epub"

const formats = new Set<DocumentFormat>(["docx", "xlsx", "pptx", "epub"])

export function fileName(path: string): string {
  const slash = path.lastIndexOf("/")
  return slash >= 0 ? path.slice(slash + 1) : path
}

export function documentFormat(path: string): DocumentFormat | undefined {
  const match = /\.([a-z0-9]+)$/i.exec(fileName(path))
  const ext = match?.[1].toLowerCase()
  return ext && formats.has(ext as DocumentFormat) ? ext as DocumentFormat : undefined
}

export function tableDelimiter(path: string): "," | "\t" | undefined {
  if (/\.csv$/i.test(fileName(path))) return ","
  if (/\.tsv$/i.test(fileName(path))) return "\t"
  return undefined
}

export function isHtmlPreviewPath(path: string): boolean {
  return /\.html?$/i.test(fileName(path))
}
