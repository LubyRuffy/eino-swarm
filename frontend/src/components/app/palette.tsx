import { useEffect, useState } from "react"
import { defaultFilter } from "cmdk"
import { Languages, Code2, MessageSquare, MessageSquarePlus, PanelLeft, Search, Settings, SquareTerminal, SunMoon, UnfoldHorizontal } from "lucide-react"

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
  onToggleTranscriptMode,
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
  onToggleTranscriptMode: () => void
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
  const loading = searching && hitsFor !== q
  const failed = searching && searchFailed && hitsFor === q
  const conversationHits = !searching
    ? null
    : searchFailed && hitsFor === q
      ? null
      : hits && hitsFor === q
        ? hits
        : []
  // Server hits already include semantic matching and ranking. Only commands
  // and the local fallback should receive cmdk's fuzzy filtering/scoring.
  const hitValues = new Set(conversationHits?.map((hit) => conversationHitValue(hit, query).trim()))

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      filter={(value, search, keywords = []) => {
        if (hitValues.has(value)) return 1
        const needle = search.trim()
        // Joining aliases and labels lets fuzzy matching bridge unrelated text.
        return Math.max(
          defaultFilter(value, needle),
          ...keywords.map((keyword) => defaultFilter(keyword, needle)),
        )
      }}
    >
      <CommandInput
        value={query}
        onValueChange={setQuery}
        placeholder={t("palette.placeholder")}
      />
      <CommandList aria-busy={loading}>
        {loading ? (
          <div role="status" className="px-3 py-2 text-sm text-muted-foreground">
            {t("palette.searching")}
          </div>
        ) : failed ? (
          <div role="alert" className="px-3 py-2 text-sm text-destructive">
            {t("palette.searchFailed")}
          </div>
        ) : (
          <CommandEmpty>{t("palette.empty")}</CommandEmpty>
        )}
        <CommandGroup heading={t("palette.actions")}>
          <CommandItem
            value="new conversation 新对话"
            keywords={[t("palette.new")]}
            onSelect={() => run(onNew)}
          >
            <MessageSquarePlus />
            {t("palette.new")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘N</kbd>
          </CommandItem>
          <CommandItem
            value="find in conversation 查找"
            keywords={[t("palette.find")]}
            onSelect={() => run(onFind)}
          >
            <Search />
            {t("palette.find")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘F</kbd>
          </CommandItem>
          <CommandItem
            value="toggle conversations sidebar 会话"
            keywords={[t("palette.toggleSidebar")]}
            onSelect={() => run(onToggleSidebar)}
          >
            <PanelLeft />
            {t("palette.toggleSidebar")}
            <kbd className="ml-auto text-[11px] text-muted-foreground">⌘B</kbd>
          </CommandItem>
          <CommandItem
            value="settings 设置"
            keywords={[t("palette.settings")]}
            onSelect={() => run(onSettings)}
          >
            <Settings />
            {t("palette.settings")}
          </CommandItem>
          <CommandItem
            value="theme appearance 主题"
            keywords={[t("palette.theme")]}
            onSelect={() => run(onToggleTheme)}
          >
            <SunMoon />
            {t("palette.theme")}
          </CommandItem>
          <CommandItem
            value="language 语言 中文 english 切换"
            keywords={[t("palette.language")]}
            onSelect={() => run(onToggleLocale)}
          >
            <Languages />
            {t("palette.language")}
          </CommandItem>
          <CommandItem
            value="width wide standard full comfortable 宽屏 标准 铺满"
            keywords={[t("palette.width")]}
            onSelect={() => run(onToggleContentWidth)}
          >
            <UnfoldHorizontal />
            {t("palette.width")}
          </CommandItem>
          <CommandItem
            value="transcript user developer compact tools thinking 用户 开发 精简"
            keywords={[t("palette.transcriptMode")]}
            onSelect={() => run(onToggleTranscriptMode)}
          >
            <Code2 />
            {t("palette.transcriptMode")}
          </CommandItem>
          <CommandItem
            value="terminal shell 终端"
            keywords={[t("palette.terminal")]}
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
