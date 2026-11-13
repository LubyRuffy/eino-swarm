import { api } from "@/lib/api"
import {
  flattenFileListings,
  pruneFileListings,
  withFileDir,
  type FileListings,
} from "@/lib/workspace-listing"
import type { FileEntry } from "@/lib/types"

type FileState = {
  activeId?: string
  workspace: string
  fileListings: FileListings
  fileDirErrors: Record<string, string>
}

type SetFileState = (partial: Partial<FileState> & {
  files?: FileEntry[]
  loadedFileDirs?: string[]
}) => void

let fileEpoch = 0
const fileTokens = new Map<string, number>()
const fileLoads = new Set<string>()

export function resetWorkspaceFileLoads(): void {
  fileEpoch += 1
  fileTokens.clear()
  fileLoads.clear()
}

function begin(dir: string): { epoch: number; token: number } {
  const token = (fileTokens.get(dir) ?? 0) + 1
  fileTokens.set(dir, token)
  return { epoch: fileEpoch, token }
}

function current(dir: string, epoch: number, token: number): boolean {
  return epoch === fileEpoch && fileTokens.get(dir) === token
}

function publish(set: SetFileState, listings: FileListings, workspace: string, errors: Record<string, string>) {
  const pruned = pruneFileListings(listings)
  set({
    fileListings: pruned,
    files: flattenFileListings(pruned),
    loadedFileDirs: Object.keys(pruned),
    fileDirErrors: errors,
    workspace,
  })
}

function failureText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export async function refreshWorkspaceFiles(get: () => FileState, set: SetFileState): Promise<void> {
  const id = get().activeId
  if (!id) return
  const wanted = [...new Set(["", ...Object.keys(get().fileListings), ...Object.keys(get().fileDirErrors)])]
  const jobs = wanted.map((dir) => {
    const stamp = begin(dir)
    return api.files(id, dir || undefined).then(
      (res) => ({ dir, ...stamp, res }),
      (error: unknown) => ({ dir, ...stamp, error }),
    )
  })
  const pages = await Promise.all(jobs)
  if (get().activeId !== id || fileEpoch !== pages[0]?.epoch) return
  let listings = { ...get().fileListings }
  let workspace = get().workspace
  const errors = { ...get().fileDirErrors }
  for (const page of pages) {
    if (!current(page.dir, page.epoch, page.token)) continue
    if ("res" in page && page.res) {
      listings[page.dir] = page.res.files
      workspace = page.res.workspace
      delete errors[page.dir]
      continue
    }
    // Keep the last successful root, but do not present that failure as an empty directory.
    errors[page.dir] = failureText("error" in page ? page.error : "")
    if (page.dir === "") continue
    delete listings[page.dir]
  }
  publish(set, listings, workspace, errors)
}

export async function ensureWorkspaceDir(get: () => FileState, set: SetFileState, dir: string): Promise<void> {
  const id = get().activeId
  if (!id || !dir) return
  if (Object.prototype.hasOwnProperty.call(get().fileListings, dir) || get().fileDirErrors[dir]) return
  const key = `${id}\0${dir}`
  if (fileLoads.has(key)) return
  fileLoads.add(key)
  const stamp = begin(dir)
  try {
    const res = await api.files(id, dir)
    if (get().activeId !== id || !current(dir, stamp.epoch, stamp.token)) return
    const errors = { ...get().fileDirErrors }
    delete errors[dir]
    publish(set, withFileDir(get().fileListings, dir, res.files), res.workspace, errors)
  } catch (error) {
    if (get().activeId !== id || !current(dir, stamp.epoch, stamp.token)) return
    set({ fileDirErrors: { ...get().fileDirErrors, [dir]: failureText(error) } })
  } finally {
    fileLoads.delete(key)
  }
}

export async function searchWorkspaceFiles(get: () => Pick<FileState, "activeId">, query: string): Promise<FileEntry[]> {
  const id = get().activeId
  if (!id) return []
  const epoch = fileEpoch
  const { files } = await api.searchFiles(id, query)
  if (get().activeId !== id || epoch !== fileEpoch) return []
  return files
}
