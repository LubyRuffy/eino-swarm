import { MessageSquare, MessageSquarePlus, Settings, SunMoon } from "lucide-react"

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import type { Thread } from "@/lib/types"
import { relativeDay } from "@/lib/utils"

/** ⌘K: jump to a conversation, or run the two or three things worth a
 *  keystroke. Search lives here rather than in the sidebar so the sidebar can
 *  stay a list. */
export function Palette({
  open,
  onOpenChange,
  threads,
  onOpen,
  onNew,
  onSettings,
  onToggleTheme,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  threads: Thread[]
  onOpen: (id: string) => void
  onNew: () => void
  onSettings: () => void
  onToggleTheme: () => void
}) {
  const run = (fn: () => void) => {
    onOpenChange(false)
    fn()
  }

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange}>
      <CommandInput placeholder="Search conversations, or type a command…" />
      <CommandList>
        <CommandEmpty>Nothing matches that.</CommandEmpty>
        <CommandGroup heading="Actions">
          <CommandItem value="new conversation" onSelect={() => run(onNew)}>
            <MessageSquarePlus />
            New conversation
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘N</kbd>
          </CommandItem>
          <CommandItem value="settings" onSelect={() => run(onSettings)}>
            <Settings />
            Settings
          </CommandItem>
          <CommandItem value="theme appearance" onSelect={() => run(onToggleTheme)}>
            <SunMoon />
            Switch between light and dark
          </CommandItem>
        </CommandGroup>
        {threads.length > 0 ? (
          <CommandGroup heading="Conversations">
            {threads.map((t) => (
              <CommandItem
                key={t.id}
                value={`${t.title} ${t.id}`}
                onSelect={() => run(() => onOpen(t.id))}
              >
                <MessageSquare />
                <span className="truncate">{t.title || "Untitled"}</span>
                <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">
                  {relativeDay(t.last_active_at)}
                </span>
              </CommandItem>
            ))}
          </CommandGroup>
        ) : null}
      </CommandList>
    </CommandDialog>
  )
}
