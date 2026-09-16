import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/** One confirm for every destructive delete. A trash icon that fires
 *  immediately is how conversations, files and providers vanish on a slip.
 *  Unmount when closed: a nested Dialog.Root inside Settings otherwise
 *  steals the parent sheet's context and the Memory tab stops switching. */
export function ConfirmDeleteDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  cancelLabel: string
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  if (!open) return null
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

