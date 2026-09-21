import { useEffect, useState } from "react"
import { Languages, MessageSquare, MessageSquarePlus, PanelLeft, Search, Settings, SquareTerminal, SunMoon, UnfoldHorizontal } from "lucide-react"

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { api } from "@/lib/api"
import {
  SEARCH_DEBOUNCE_MS,
  conversationHitValue,
  shouldQuerySearch,
} from "@/lib/thread-search"
import type { SearchHit, Thread } from "@/lib/types"
import { relativeDay } from "@/lib/utils"
import { useT } from "@/lib/use-t"

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
  onToggleLocale,
  onToggleContentWidth,
  onToggleSidebar,
  onFind,
  onOpenTerminal,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  threads: Thread[]
  onOpen: (id: string) => void
  onNew: () => void
  onSettings: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onToggleContentWidth: () => void
  onToggleSidebar: () => void
  onFind: () => void
  onOpenTerminal: () => void
}) {
  const t = useT()
  const [query, setQuery] = useState("")
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [hitsFor, setHitsFor] = useState("")
  const [searchFailed, setSearchFailed] = useState(false)
  const run = (fn: () => void) => {
    onOpenChange(false)
    fn()
  }

  useEffect(() => {
    if (!open) {
      setQuery("")
      setHits(null)
      setHitsFor("")
      setSearchFailed(false)
      return
    }
    if (!shouldQuerySearch(query)) {
      setHits(null)
      setHitsFor("")
      setSearchFailed(false)
      return
    }
    const q = query.trim()
    setHits(null)
    setHitsFor("")
    setSearchFailed(false)
    let cancelled = false
    const handle = window.setTimeout(() => {
      void api
        .search(q)
        .then((res) => {
          if (cancelled) return
          setHits(res.hits ?? [])
          setHitsFor(q)
          setSearchFailed(false)
        })
        .catch(() => {
          if (cancelled) return
          setHits(null)
          setHitsFor(q)
          setSearchFailed(true)
        })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      cancelled = true
      window.clearTimeout(handle)
    }
  }, [open, query])

  const searching = shouldQuerySearch(query)
  const q = query.trim()
  const conversationHits = !searching
    ? null
    : searchFailed && hitsFor === q
      ? null
      : hits && hitsFor === q
        ? hits
        : []

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} shouldFilter={!searching}>
      <CommandInput
        value={query}
        onValueChange={setQuery}
        placeholder={t("palette.placeholder")}
      />
      <CommandList>
        <CommandEmpty>{t("palette.empty")}</CommandEmpty>
        <CommandGroup heading={t("palette.actions")}>
          <CommandItem value="new conversation 新对话" onSelect={() => run(onNew)}>
            <MessageSquarePlus />
            {t("palette.new")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘N</kbd>
          </CommandItem>
          <CommandItem value="find in conversation 查找" onSelect={() => run(onFind)}>
            <Search />
            {t("palette.find")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘F</kbd>
          </CommandItem>
          <CommandItem value="toggle conversations sidebar 会话" onSelect={() => run(onToggleSidebar)}>
            <PanelLeft />
            {t("palette.toggleSidebar")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘B</kbd>
          </CommandItem>
          <CommandItem value="settings 设置" onSelect={() => run(onSettings)}>
            <Settings />
            {t("palette.settings")}
          </CommandItem>
          <CommandItem value="theme appearance 主题" onSelect={() => run(onToggleTheme)}>
            <SunMoon />
            {t("palette.theme")}
          </CommandItem>
          <CommandItem
            value="language 语言 中文 english 切换"
            onSelect={() => run(onToggleLocale)}
          >
            <Languages />
            {t("palette.language")}
          </CommandItem>
          <CommandItem
            value="width wide standard full comfortable 宽屏 标准 铺满"
            onSelect={() => run(onToggleContentWidth)}
          >
            <UnfoldHorizontal />
            {t("palette.width")}
          </CommandItem>
          <CommandItem
            value="terminal shell 终端"
            onSelect={() => run(onOpenTerminal)}
          >
            <SquareTerminal />
            {t("palette.terminal")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘J</kbd>
          </CommandItem>
        </CommandGroup>
        {conversationHits ? (
          conversationHits.length > 0 ? (
            <CommandGroup heading={t("palette.conversations")}>
              {conversationHits.map((hit) => (
                <CommandItem
                  key={hit.thread_id}
                  value={conversationHitValue(hit, query)}
                  onSelect={() => run(() => onOpen(hit.thread_id))}
                >
                  <MessageSquare />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{hit.title || t("palette.untitled")}</span>
                    {hit.snippet ? (
                      <span className="block truncate text-[11px] text-muted-foreground">
                        {hit.snippet}
                      </span>
                    ) : null}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          ) : null
        ) : threads.length > 0 ? (
          <CommandGroup heading={t("palette.conversations")}>
            {threads.map((thread) => (
              <CommandItem
                key={thread.id}
                value={`${thread.title} ${thread.id}`}
                onSelect={() => run(() => onOpen(thread.id))}
              >
                <MessageSquare />
                <span className="truncate">{thread.title || t("palette.untitled")}</span>
                <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">
                  {relativeDay(thread.last_active_at, new Date(), t.locale)}
                </span>
              </CommandItem>
            ))}
          </CommandGroup>
        ) : null}
      </CommandList>
    </CommandDialog>
  )
}
