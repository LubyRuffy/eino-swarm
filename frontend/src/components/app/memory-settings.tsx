import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import type { Settings } from "@/lib/types"

/** The install-wide memory budget. Per-project memory is switched on in the
 *  project dialog; these numbers decide what it costs when it is. */
export function MemorySettings({
  settings,
  onChange,
}: {
  settings: Settings
  onChange: (s: Settings) => void
}) {
  const update = (patch: Partial<Settings["memory"]>) =>
    onChange({ ...settings, memory: { ...settings.memory, ...patch } })

  return (
    <div className="space-y-4">
      <Toggle
        id="memory-enabled"
        label="Remember anything at all"
        hint="Off means no project carries notes or skills, whatever its own switch says."
        checked={settings.memory.enabled}
        onCheckedChange={(enabled) => update({ enabled })}
      />
      <Toggle
        id="memory-auto-review"
        label="Review a conversation when it finishes"
        hint="A finished turn is read back so durable facts and reusable procedures are kept. Off means memory only changes when an agent or you write to it."
        checked={settings.memory.auto_review}
        disabled={!settings.memory.enabled}
        onCheckedChange={(auto_review) => update({ auto_review })}
      />

      <div className="space-y-1.5">
        <Label htmlFor="memory-char-limit">Notes budget (characters)</Label>
        <Input
          id="memory-char-limit"
          type="number"
          min={200}
          value={settings.memory.char_limit}
          onChange={(e) => update({ char_limit: Number(e.target.value) })}
        />
        <p className="text-xs text-muted-foreground">
          Every note is in the prompt of every turn in the project, so this is
          a per-turn cost. Once it is full, an agent must replace a note to add
          one.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="memory-review-iterations">Review tool rounds</Label>
        <Input
          id="memory-review-iterations"
          type="number"
          min={1}
          value={settings.memory.review_max_iterations}
          onChange={(e) =>
            update({ review_max_iterations: Number(e.target.value) })
          }
        />
        <p className="text-xs text-muted-foreground">
          How many times the review may think and write before it is stopped.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="memory-skills-index">Skills listed in the prompt</Label>
        <Input
          id="memory-skills-index"
          type="number"
          min={1}
          value={settings.memory.skills_index_max}
          onChange={(e) => update({ skills_index_max: Number(e.target.value) })}
        />
        <p className="text-xs text-muted-foreground">
          Only names and one-line descriptions are listed; an agent opens the
          one it needs.
        </p>
      </div>
    </div>
  )
}

function Toggle({
  id,
  label,
  hint,
  checked,
  disabled,
  onCheckedChange,
}: {
  id: string
  label: string
  hint: string
  checked: boolean
  disabled?: boolean
  onCheckedChange: (on: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <Label htmlFor={id}>{label}</Label>
        <p className="mt-1 text-xs text-muted-foreground">{hint}</p>
      </div>
      <Switch
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={onCheckedChange}
      />
    </div>
  )
}
