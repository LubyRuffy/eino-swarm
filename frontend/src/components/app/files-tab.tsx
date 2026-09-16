import {
  ChevronRight,
  Download,
  File as FileIcon,
  FileCode,
  FileImage,
  FileJson,
  FileText,
  Folder,
  FolderOpen,
  RefreshCw,
  Search,
  Trash2,
  Upload,
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"

import { CopyButton } from "@/components/app/transcript"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import {
  buildFileTree,
  fileKind,
  fileTreeKey,
  initialExpanded,
  pruneExpanded,
  toggleExpanded,
  visibleRows,
  type FileKind,
  type FileTreeNode,
} from "@/lib/file-tree"
import type { FileEntry } from "@/lib/types"
import { cn, formatBytes } from "@/lib/utils"
import { useT } from "@/lib/use-t"

const kindClass: Record<FileKind, string> = {
  folder: "text-file-folder",
  code: "text-file-code",
  doc: "text-file-doc",
  config: "text-file-config",
  image: "text-file-image",
  file: "text-muted-foreground",
}

export function FilesTab({
  files,
  workspace,
  threadId,
  canReveal,
  onUpload,
  onDelete,
  onRefresh,
  onReveal,
}: {
  files: FileEntry[]
  workspace: string
  threadId?: string
  canReveal: boolean
  onUpload: (files: File[]) => Promise<unknown>
  onDelete: (path: string) => void
  onRefresh: () => void
  onReveal: (path?: string) => void
}) {
  const t = useT()
  const inputRef = useRef<HTMLInputElement>(null)
  const userTouched = useRef(false)
  const [filter, setFilter] = useState("")
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [focused, setFocused] = useState<string>()
  const [doomed, setDoomed] = useState<string>()
  const tree = useMemo(() => buildFileTree(files), [files])
  const rows = useMemo(
    () => visibleRows(tree, expanded, filter),
    [tree, expanded, filter],
  )
  const filtering = filter.trim().length > 0

  useEffect(() => {
    userTouched.current = false
    setFilter("")
    setFocused(undefined)
  }, [threadId])

  useEffect(() => {
    if (!userTouched.current) {
      setExpanded(initialExpanded(tree))
      return
    }
    setExpanded((prev) => pruneExpanded(prev, tree))
  }, [threadId, tree])

  const applyToggle = (path: string) => {
    userTouched.current = true
    setExpanded((prev) => toggleExpanded(prev, path))
  }

  const openFile = (path: string) => {
    if (canReveal) onReveal(path)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-col gap-2 px-2 pb-2 pt-2">
        <div className="flex items-center gap-1">
          <input
            ref={inputRef}
            type="file"
            multiple
            className="hidden"
            onChange={async (e) => {
              const picked = Array.from(e.target.files ?? [])
              e.target.value = ""
              if (picked.length > 0) await onUpload(picked)
            }}
          />
          <Button
            variant="ghost"
            size="sm"
            className="gap-1.5"
            onClick={() => inputRef.current?.click()}
          >
            <Upload />
            {t("files.upload")}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={onRefresh} title={t("files.refresh")}>
            <RefreshCw />
          </Button>
          {canReveal ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onReveal()}
              title={t("files.reveal")}
            >
              <FolderOpen />
            </Button>
          ) : null}
          {workspace ? <CopyButton text={workspace} className="ml-auto" /> : null}
        </div>
        {files.length > 0 ? (
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="file-filter"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              aria-label={t("files.filter")}
              placeholder={t("files.filterPlaceholder")}
              spellCheck={false}
              className="h-8 pl-7 text-xs shadow-none"
            />
          </div>
        ) : null}
      </div>

      {files.length === 0 ? (
        <p className="px-3 py-8 text-center text-xs text-muted-foreground">
          {t("files.empty")}
        </p>
      ) : rows.length === 0 ? (
        <p className="px-3 py-8 text-center text-xs text-muted-foreground">
          {t("files.noMatch")}
        </p>
      ) : (
        <div
          data-testid="file-tree"
          role="tree"
          aria-label={t("files.tree")}
          tabIndex={0}
          className="thin-scrollbar min-h-0 flex-1 overflow-y-auto px-1 pb-2 outline-none"
          onKeyDown={(e) => {
            const action = fileTreeKey(e.key, rows, focused, expanded, filtering)
            if (!action) return
            e.preventDefault()
            if (action.type === "focus") setFocused(action.path)
            if (action.type === "toggle") {
              setFocused(action.path)
              applyToggle(action.path)
            }
            if (action.type === "open") {
              setFocused(action.path)
              openFile(action.path)
            }
          }}
        >
          {rows.map(({ node, depth }) => (
            <FileRow
              key={node.path}
              node={node}
              depth={depth}
              open={expanded.has(node.path)}
              focused={focused === node.path}
              threadId={threadId}
              filtering={filtering}
              onFocus={setFocused}
              onToggle={applyToggle}
              onOpen={openFile}
              onDelete={setDoomed}
            />
          ))}
        </div>
      )}
      <ConfirmDeleteDialog
        open={Boolean(doomed)}
        title={t("file.deleteTitle", {
          name: doomed ? doomed.slice(doomed.lastIndexOf("/") + 1) : "",
        })}
        description={t("file.deleteDesc")}
        confirmLabel={t("file.deleteConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={(open) => {
          if (!open) setDoomed(undefined)
        }}
        onConfirm={() => {
          if (doomed) onDelete(doomed)
        }}
      />
    </div>
  )
}

function FileRow({
  node,
  depth,
  open,
  focused,
  threadId,
  filtering,
  onFocus,
  onToggle,
  onOpen,
  onDelete,
}: {
  node: FileTreeNode
  depth: number
  open: boolean
  focused: boolean
  threadId?: string
  filtering: boolean
  onFocus: (path: string) => void
  onToggle: (path: string) => void
  onOpen: (path: string) => void
  onDelete: (path: string) => void
}) {
  const t = useT()
  const expanded = node.dir ? filtering || open : undefined
  const title = node.dir
    ? node.path
    : `${node.path} · ${formatBytes(node.size)}`

  return (
    <div
      role="treeitem"
      aria-label={node.name}
      aria-expanded={expanded}
      aria-level={depth + 1}
      aria-selected={focused}
      data-path={node.path}
      className={cn(
        "group flex h-7 cursor-default items-center gap-1 rounded-md pr-1 text-[13px] hover:bg-accent/60",
        focused && "bg-accent",
      )}
      style={{ paddingLeft: 6 + depth * 12 }}
      onClick={() => {
        onFocus(node.path)
        if (node.dir) onToggle(node.path)
      }}
      onDoubleClick={() => {
        if (!node.dir) onOpen(node.path)
      }}
    >
      <span className="flex size-3.5 shrink-0 items-center justify-center">
        {node.dir ? (
          <ChevronRight
            className={cn(
              "size-3 text-muted-foreground transition-transform",
              expanded && "rotate-90",
            )}
          />
        ) : null}
      </span>
      <FileGlyph node={node} open={Boolean(expanded)} />
      <span className="min-w-0 flex-1 truncate" title={title}>
        {node.name}
      </span>
      {node.uploaded ? (
        <Badge variant="outline" className="shrink-0 px-1 py-0 text-[10px]">
          {t("files.yours")}
        </Badge>
      ) : null}
      {!node.dir && threadId ? (
        <div
          className="flex shrink-0 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
          onClick={(e) => e.stopPropagation()}
        >
          <Button variant="ghost" size="icon-sm" asChild title={t("files.download")}>
            <a href={api.downloadURL(threadId, node.path)} download>
              <Download />
            </a>
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            title={t("files.delete")}
            onClick={() => onDelete(node.path)}
          >
            <Trash2 />
          </Button>
        </div>
      ) : null}
    </div>
  )
}

function FileGlyph({ node, open }: { node: FileTreeNode; open: boolean }) {
  const kind = fileKind(node)
  const className = cn("size-3.5 shrink-0", kindClass[kind])
  if (node.dir) {
    return open ? (
      <FolderOpen className={className} />
    ) : (
      <Folder className={className} />
    )
  }
  if (kind === "code") return <FileCode className={className} />
  if (kind === "doc") return <FileText className={className} />
  if (kind === "config") return <FileJson className={className} />
  if (kind === "image") return <FileImage className={className} />
  return <FileIcon className={className} />
}
