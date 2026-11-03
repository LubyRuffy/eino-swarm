export type InlineRun = { text: string; href?: string }

export type DocBlock =
  | { kind: "heading"; level: number; text: string }
  | { kind: "paragraph"; runs: InlineRun[] }
  | { kind: "list"; ordered: boolean; items: InlineRun[][] }
  | { kind: "table"; header: boolean; rows: string[][] }
  | { kind: "image"; alt: string; dataUrl: string }

export type DocPreview = { blocks: DocBlock[] }
export type SheetPreview = { name: string; rows: string[][] }
export type WorkbookPreview = { sheets: SheetPreview[]; truncated: boolean; shown: number }
export type SlidePreview = { title: string; blocks: DocBlock[] }
export type DeckPreview = { slides: SlidePreview[] }
export type ChapterPreview = { title: string; srcdoc: string }
export type BookPreview = { title: string; chapters: ChapterPreview[] }
