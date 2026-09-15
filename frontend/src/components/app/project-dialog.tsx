import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { ApiError, type ProjectPatch } from "@/lib/api"
import type { Project } from "@/lib/types"

/** Create or edit one project: its instruction, its working directory and
 *  whether it remembers anything. */
export function ProjectDialog({
  open,
  project,
  memoryAvailable,
  onOpenChange,
  onSave,
}: {
  open: boolean
  /** Undefined creates a project; a project edits it. */
  project?: Project
  /** False when memory is off for the whole install, so the per-project
   *  switch does not promise something the app will not do. */
  memoryAvailable: boolean
  onOpenChange: (open: boolean) => void
  onSave: (patch: ProjectPatch) => Promise<Project>
}) {
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [workdir, setWorkdir] = useState("")
  const [memory, setMemory] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [workdirError, setWorkdirError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setName(project?.name ?? "")
    setPrompt(project?.system_prompt ?? "")
    setWorkdir(project?.workdir ?? "")
    setMemory(project ? project.memory_enabled : memoryAvailable)
    setError(undefined)
    setWorkdirError(undefined)
  }, [open, project, memoryAvailable])

  const submit = async () => {
    setSaving(true)
    setError(undefined)
    setWorkdirError(undefined)
    try {
      await onSave({
        name: name.trim(),
        system_prompt: prompt,
        workdir: workdir.trim(),
        memory_enabled: memory,
      })
      onOpenChange(false)
    } catch (e) {
      // A refused working directory is shown under the field that caused it;
      // anything else goes above the buttons, where the eye already is.
      if (e instanceof ApiError && e.code === "workdir") {
        setWorkdirError(e.message)
      } else {
        setError(e instanceof Error ? e.message : String(e))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{project ? "Edit project" : "New project"}</DialogTitle>
          <DialogDescription>
            Conversations in a project share a working directory, an
            instruction, and what earlier conversations learned.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="project-name">Name</Label>
            <Input
              id="project-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="project-prompt">Instruction</Label>
            <Textarea
              id="project-prompt"
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Added to the system prompt of every conversation in this project.
            </p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="project-workdir">Working directory</Label>
            <Input
              id="project-workdir"
              value={workdir}
              placeholder="Leave empty and zwai manages one"
              onChange={(e) => setWorkdir(e.target.value)}
              aria-invalid={Boolean(workdirError)}
            />
            {workdirError ? (
              <p className="text-xs text-destructive">{workdirError}</p>
            ) : (
              <p className="text-xs text-muted-foreground">
                An absolute path that already exists. The agents read and write
                it directly, with your permissions.
              </p>
            )}
          </div>

          <div className="flex items-start justify-between gap-4 rounded-md border border-border p-3">
            <div className="min-w-0">
              <Label htmlFor="project-memory">Memory</Label>
              <p className="mt-1 text-xs text-muted-foreground">
                {memoryAvailable
                  ? "After each conversation, keep what is worth carrying forward: notes and reusable procedures."
                  : "Memory is switched off for this install; turn it on in Settings first."}
              </p>
              {project ? (
                <p className="mt-1 break-all text-xs text-muted-foreground">
                  Stored in {project.memory_dir}
                </p>
              ) : null}
            </div>
            <Switch
              id="project-memory"
              checked={memory && memoryAvailable}
              disabled={!memoryAvailable}
              onCheckedChange={setMemory}
            />
          </div>
        </div>

        {error ? <p className="text-sm text-destructive">{error}</p> : null}

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={saving || name.trim() === ""}
            onClick={() => void submit()}
          >
            {project ? "Save" : "Create project"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
