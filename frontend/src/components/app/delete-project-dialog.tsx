import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import type { Project } from "@/lib/types"

/** Deleting a project takes its conversations with it, which is not what the
 *  word suggests — so it is spelled out, along with what survives. */
export function DeleteProjectDialog({
  project,
  onOpenChange,
  onConfirm,
}: {
  /** Undefined keeps the dialog closed. */
  project?: Project
  onOpenChange: (open: boolean) => void
  onConfirm: (project: Project) => void
}) {
  return (
    <Dialog open={Boolean(project)} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Delete {project?.name}?</DialogTitle>
          <DialogDescription>
            Its conversations and everything it remembered are deleted with it.
            {project?.workdir
              ? " The files in its working directory are left alone."
              : ""}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              if (project) onConfirm(project)
              onOpenChange(false)
            }}
          >
            Delete project
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
