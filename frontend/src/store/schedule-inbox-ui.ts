import { create } from "zustand"

import { runsInFromSchedule, RUNS_IN_NEW } from "@/lib/schedule-dest"
import {
  cadenceFormFields,
  type InboxFilter,
  type ScheduleCadenceKind,
} from "@/lib/schedule-view"
import type { Schedule, ScheduleRun } from "@/lib/types"

export type InboxForm = {
  title: string
  prompt: string
  cadence: ScheduleCadenceKind
  cadenceValue: string
  projectId: string
  runsIn: string
}

export type InboxDrawer = { mode: "create" } | { mode: "edit"; id: string }

export function emptyInboxForm(): InboxForm {
  return {
    title: "",
    prompt: "",
    cadence: "delay",
    cadenceValue: "",
    projectId: "none",
    runsIn: RUNS_IN_NEW,
  }
}

export function inboxFormFromRow(row: Schedule): InboxForm {
  const fields = cadenceFormFields(row)
  return {
    title: row.title ?? "",
    prompt: row.prompt ?? "",
    cadence: fields.cadence,
    cadenceValue: fields.value,
    projectId: (row.project_id ?? "").trim() || "none",
    runsIn: runsInFromSchedule(row),
  }
}

type InboxUI = {
  drawer: InboxDrawer | null
  expanded: boolean
  filter: InboxFilter
  query: string
  form: InboxForm
  runs: ScheduleRun[]
  reset: () => void
  setQuery: (query: string) => void
  setFilter: (filter: InboxFilter) => void
  setExpanded: (next: boolean | ((on: boolean) => boolean)) => void
  patchForm: (partial: Partial<InboxForm>) => void
  setRuns: (runs: ScheduleRun[]) => void
  openCreate: () => void
  openEdit: (row: Schedule) => void
  closeDrawer: () => void
}

const fresh = () => ({
  drawer: null as InboxDrawer | null,
  expanded: false,
  filter: "active" as InboxFilter,
  query: "",
  form: emptyInboxForm(),
  runs: [] as ScheduleRun[],
})

export const useScheduleInboxUI = create<InboxUI>((set) => ({
  ...fresh(),
  reset: () => set(fresh()),
  setQuery: (query) => set({ query }),
  setFilter: (filter) => set({ filter }),
  setExpanded: (next) =>
    set((s) => ({ expanded: typeof next === "function" ? next(s.expanded) : next })),
  patchForm: (partial) => set((s) => ({ form: { ...s.form, ...partial } })),
  setRuns: (runs) => set({ runs }),
  openCreate: () =>
    set((s) => {
      if (s.drawer?.mode === "create") return { drawer: null, expanded: false }
      return { drawer: { mode: "create" }, expanded: false, form: emptyInboxForm() }
    }),
  openEdit: (row) =>
    set((s) => {
      if (s.drawer?.mode === "edit" && s.drawer.id === row.id) {
        return { drawer: null, expanded: false }
      }
      return {
        drawer: { mode: "edit", id: row.id },
        expanded: false,
        form: inboxFormFromRow(row),
      }
    }),
  closeDrawer: () => set({ drawer: null, expanded: false }),
}))

export function resetScheduleInboxUI() {
  useScheduleInboxUI.getState().reset()
}
