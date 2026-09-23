import { ChevronLeft, Monitor } from "lucide-react"
import { useState } from "react"

import { ChoiceRail } from "@/components/choice-rail"
import { Composer, type ComposerExtra } from "@/components/composer"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import type { ModelChoice, ProjectView } from "@/lib/rpc"
import { linkLabel, type SavedLink } from "@/lib/store"

/** Starting a conversation is its own screen: which PC and which project it
 *  lands in are the first two decisions, and the inbox is no place to make
 *  them while reading what is already running. */
export function NewChatScreen({
  hosts,
  activeFingerprint,
  projects,
  initialProject = "",
  connected = true,
  connecting = false,
  models,
  reasoningLevels,
  catalogBusy,
  pending,
  onSelectHost,
  onBack,
  onStart,
}: {
  hosts: SavedLink[]
  activeFingerprint: string
  projects: ProjectView[]
  initialProject?: string
  connected?: boolean
  connecting?: boolean
  models?: ModelChoice[]
  reasoningLevels?: string[]
  catalogBusy?: boolean
  pending?: boolean
  onSelectHost: (fingerprint: string) => void
  onBack: () => void
  onStart: (text: string, projectId: string, extra?: ComposerExtra) => void
}) {
  // A project id from another PC must not ride in as a selection this PC
  // cannot honour.
  const [project, setProject] = useState(() =>
    projects.some((p) => p.id === initialProject) ? initialProject : "",
  )
  return (
    // mx-auto on a flex child does not stretch. Without a definite width the
    // chip row and the composer size themselves, and whatever passes the
    // edge cannot be scrolled back.
    <main className="mx-auto flex h-full min-w-0 w-full max-w-lg flex-col overflow-hidden">
      <header className="flex h-14 shrink-0 items-center gap-1 border-b border-border px-2">
        <Button
          type="button"
          variant="ghost"
          className="size-10 shrink-0 px-0"
          aria-label={t("thread.back")}
          onClick={onBack}
        >
          <ChevronLeft className="size-5" aria-hidden />
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-[15px] font-medium">{t("compose.title")}</h1>
      </header>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-5 overflow-x-hidden overflow-y-auto px-3 py-4">
        <p className="px-1 text-[13px] text-muted-foreground">{t("compose.hint")}</p>
        <ChoiceRail
          label={t("home.hosts")}
          value={activeFingerprint}
          choices={hosts.map((h) => ({
            id: h.fingerprint,
            name: linkLabel(h, hosts),
            icon: <Monitor className="size-3.5 shrink-0" aria-hidden />,
          }))}
          onChange={(fp) => {
            if (fp === activeFingerprint) return
            // Project ids belong to one PC. Carrying the last pick across a
            // switch would start the conversation in a project this PC
            // never heard of.
            setProject("")
            onSelectHost(fp)
          }}
        />
        <ChoiceRail
          label={t("home.project")}
          value={project}
          choices={[{ id: "", name: t("home.defaultProject") }, ...projects]}
          onChange={setProject}
        />
        {!connected && !connecting ? (
          <p className="px-1 text-[13px] text-destructive">{t("compose.offline")}</p>
        ) : null}
      </div>

      <Composer
        label={t("home.newMessage")}
        sendLabel={t("home.start")}
        disabled={!connected}
        pending={pending}
        hint={connecting ? t("scan.connecting") : undefined}
        models={models}
        reasoningLevels={reasoningLevels}
        catalogBusy={catalogBusy}
        onSubmit={(text, extra) => (extra ? onStart(text, project, extra) : onStart(text, project))}
      />
    </main>
  )
}
