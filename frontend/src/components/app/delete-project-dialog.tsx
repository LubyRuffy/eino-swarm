import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import type { Project } from "@/lib/types"
import { useT } from "@/lib/use-t"

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
  const t = useT()
  return (
    <ConfirmDeleteDialog
      open={Boolean(project)}
      title={t("project.deleteTitle", { name: project?.name ?? "" })}
      description={
        t("project.deleteDesc") +
        (project?.workdir ? t("project.deleteKeepFiles") : "")
      }
      confirmLabel={t("project.deleteConfirm")}
      cancelLabel={t("project.cancel")}
      onOpenChange={onOpenChange}
      onConfirm={() => {
        if (project) onConfirm(project)
      }}
    />
  )
}
