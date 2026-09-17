import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

export function SwarmTab({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const update = (patch: Partial<Settings["swarm"]>) =>
    onChange({ ...settings, swarm: { ...settings.swarm, ...patch } })
  return (
    <SettingsPage
      title={t("settings.swarm.title")}
      description={t("settings.swarm.desc")}
    >
      <SettingsSection title={t("settings.swarm.conversations")}>
        <Field
          query={query}
          label={t("settings.swarm.autoTitle")}
          hint={t("settings.swarm.autoTitleHint")}
        >
          <Switch
            checked={settings.swarm.auto_title !== false}
            onCheckedChange={(auto_title) => update({ auto_title })}
          />
        </Field>
      </SettingsSection>

      <SettingsSection title={t("settings.swarm.subagents")}>
        <Field
          query={query}
          label={t("settings.swarm.maxConcurrent")}
          hint={t("settings.swarm.maxConcurrentHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.max_concurrent}
            onChange={(e) => update({ max_concurrent: Number(e.target.value) })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.agentTimeout")}
          hint={t("settings.swarm.agentTimeoutHint")}
        >
          <Input
            type="number"
            min={30}
            value={settings.swarm.agent_timeout_seconds}
            onChange={(e) =>
              update({ agent_timeout_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.maxTurns")}
          hint={t("settings.swarm.maxTurnsHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.max_turns}
            onChange={(e) => update({ max_turns: Number(e.target.value) })}
          />
        </Field>
      </SettingsSection>

      <SettingsSection title={t("settings.swarm.manager")}>
        <Field
          query={query}
          label={t("settings.swarm.managerRounds")}
          hint={t("settings.swarm.managerRoundsHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.manager_max_iterations}
            onChange={(e) =>
              update({ manager_max_iterations: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.pulse")}
          hint={t("settings.swarm.pulseHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.progress_interval_seconds}
            onChange={(e) =>
              update({ progress_interval_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.coalesce")}
          hint={t("settings.swarm.coalesceHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.delta_coalesce_ms}
            onChange={(e) =>
              update({ delta_coalesce_ms: Number(e.target.value) })
            }
          />
        </Field>
      </SettingsSection>

      <SettingsSection title={t("settings.swarm.context")}>
        <Field
          query={query}
          label={t("settings.swarm.budget")}
          hint={t("settings.swarm.budgetHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.context_char_budget}
            onChange={(e) =>
              update({ context_char_budget: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.keep")}
          hint={t("settings.swarm.keepHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.compact_keep_messages}
            onChange={(e) =>
              update({ compact_keep_messages: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.autoCompact")}
          hint={t("settings.swarm.autoCompactHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.auto_compact_tokens}
            onChange={(e) =>
              update({ auto_compact_tokens: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.goalTurns")}
          hint={t("settings.swarm.goalTurnsHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.goal_max_auto_turns}
            onChange={(e) =>
              update({ goal_max_auto_turns: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.goalSessionSeconds")}
          hint={t("settings.swarm.goalSessionSecondsHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.goal_session_max_seconds}
            onChange={(e) =>
              update({ goal_session_max_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.goalSessionIters")}
          hint={t("settings.swarm.goalSessionItersHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.goal_session_max_iterations}
            onChange={(e) =>
              update({ goal_session_max_iterations: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.goalCompactPct")}
          hint={t("settings.swarm.goalCompactPctHint")}
        >
          <Input
            type="number"
            min={1}
            max={100}
            value={settings.swarm.goal_auto_compact_percent}
            onChange={(e) =>
              update({ goal_auto_compact_percent: Number(e.target.value) })
            }
          />
        </Field>
      </SettingsSection>
    </SettingsPage>
  )
}
