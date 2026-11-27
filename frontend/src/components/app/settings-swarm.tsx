import { useId, useState } from "react"

import { Input } from "@/components/ui/input"
import { SelectItem } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { MANAGER_ROUNDS_SETTINGS_KEY } from "@/lib/manager-rounds"
import { MAX_COMPLETION_SETTINGS_KEY } from "@/lib/output-budget"
import { AGENT_MAX_TURNS_SETTINGS_KEY, AGENT_TIMEOUT_SETTINGS_KEY } from "@/lib/agent-failure"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import { Field, SettingsChoice, SettingsPage, SettingsSection } from "./settings-field"

function SubagentCapInput({ id, value, onChange }: {
  id?: string
  value: number
  onChange: (cap: number) => void
}) {
  const t = useT()
  const errorId = useId()
  const [invalidDraft, setInvalidDraft] = useState<string>()
  const invalid = invalidDraft !== undefined
  return (
    <div className="flex flex-col items-end gap-1">
      <Input
        id={id}
        type="number"
        min={1}
        value={invalidDraft ?? value}
        aria-invalid={invalid || undefined}
        aria-describedby={invalid ? errorId : undefined}
        onChange={(event) => {
          const cap = event.currentTarget.valueAsNumber
          if (!Number.isSafeInteger(cap) || cap < 1) {
            setInvalidDraft(event.currentTarget.value)
            return
          }
          setInvalidDraft(undefined)
          onChange(cap)
        }}
      />
      {invalid && (
        <p id={errorId} role="alert" className="max-w-44 text-right text-xs text-destructive">
          {t("settings.swarm.maxConcurrentInvalid")}
        </p>
      )}
    </div>
  )
}

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
    onChange({
      ...settings,
      swarm: {
        ...settings.swarm,
        schedule_min_interval_seconds:
          settings.swarm.schedule_min_interval_seconds ?? 30,
        schedule_tick_ms: settings.swarm.schedule_tick_ms ?? 1000,
        schedule_max_active: settings.swarm.schedule_max_active ?? 32,
        ...patch,
      },
    })
  return (
    <SettingsPage
      title={t("settings.swarm.title")}
      description={t("settings.swarm.desc")}
    >

      <SettingsSection title={t("settings.swarm.subagents")}>
        <Field
          query={query}
          label={t("settings.swarm.maxConcurrent")}
          hint={t("settings.swarm.maxConcurrentHint")}
        >
          <SubagentCapInput
            value={settings.swarm.max_concurrent}
            onChange={(max_concurrent) => update({ max_concurrent })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.agentTimeout")}
          hint={t("settings.swarm.agentTimeoutHint")}
          anchor={AGENT_TIMEOUT_SETTINGS_KEY}
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
          anchor={AGENT_MAX_TURNS_SETTINGS_KEY}
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
          anchor={MANAGER_ROUNDS_SETTINGS_KEY}
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
      </SettingsSection>

      <SettingsSection title={t("settings.swarm.context")}>
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
          anchor={MAX_COMPLETION_SETTINGS_KEY}
          label={t("settings.swarm.maxCompletion")}
          hint={t("settings.swarm.maxCompletionHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.max_completion_tokens}
            onChange={(e) =>
              update({ max_completion_tokens: Number(e.target.value) })
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
      </SettingsSection>

      <SettingsSection title={t("settings.swarm.schedules")}>
        <Field
          query={query}
          label={t("settings.swarm.scheduleMin")}
          hint={t("settings.swarm.scheduleMinHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.schedule_min_interval_seconds ?? 30}
            onChange={(e) =>
              update({ schedule_min_interval_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.scheduleMax")}
          hint={t("settings.swarm.scheduleMaxHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.swarm.schedule_max_active ?? 32}
            onChange={(e) =>
              update({ schedule_max_active: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.swarm.scheduleAnswerCheck")}
          hint={t("settings.swarm.scheduleAnswerCheckHint")}
        >
          <Switch
            checked={settings.swarm.schedule_answer_check !== false}
            onCheckedChange={(schedule_answer_check) => update({ schedule_answer_check })}
          />
        </Field>
        <SettingsChoice
          query={query}
          label={t("settings.swarm.scheduleAnswerModel")}
          hint={t("settings.swarm.scheduleAnswerModelHint")}
          value={settings.swarm.schedule_answer_model ?? "chat"}
          disabled={settings.swarm.schedule_answer_check === false}
          onValueChange={(value) => {
            if (value === "auto" || value === "decision" || value === "chat") update({ schedule_answer_model: value })
          }}
        >
          <SelectItem value="auto">{t("settings.swarm.scheduleAnswerAuto")}</SelectItem>
          <SelectItem value="decision">{t("settings.swarm.scheduleAnswerDecision")}</SelectItem>
          <SelectItem value="chat">{t("settings.swarm.scheduleAnswerChat")}</SelectItem>
        </SettingsChoice>
        <SettingsChoice
          query={query}
          label={t("settings.swarm.policyThinking")}
          hint={t("settings.swarm.policyThinkingHint")}
          value={settings.swarm.policy_reasoning_effort || "default"}
          onValueChange={(effort) =>
            update({ policy_reasoning_effort: effort === "default" ? "" : effort })
          }
        >
          <SelectItem value="default">{t("settings.swarm.policyThinkingDefault")}</SelectItem>
          <SelectItem value="inherit">{t("settings.swarm.policyThinkingInherit")}</SelectItem>
          <SelectItem value="none">{t("reason.none")}</SelectItem>
          <SelectItem value="low">{t("settings.swarm.policyThinkingLow")}</SelectItem>
          <SelectItem value="medium">{t("settings.swarm.policyThinkingMedium")}</SelectItem>
          <SelectItem value="high">{t("settings.swarm.policyThinkingHigh")}</SelectItem>
        </SettingsChoice>
      </SettingsSection>
    </SettingsPage>
  )
}
