import type { ReactNode } from "react"

import { DirectChatScreen } from "@/components/direct-chat-screen"
import { UpdateNotice } from "@/components/update-notice"
import type { DirectProvider } from "@/lib/direct-provider"
import type { SavedLink } from "@/lib/store"

/** The no-PC chat shell. Split out so the inbox file stays under the line cap. */
export function ChatSurface({
  locale,
  hosts,
  activeFingerprint,
  path,
  connected,
  reconnecting,
  providers,
  banner,
  sheet,
  modelSheet,
  onSelectHost,
  onAddHost,
  onUnlink,
  onToggleLocale,
  onModels,
  onOpenChange,
  onBindClose,
}: {
  locale: string
  hosts: SavedLink[]
  activeFingerprint: string
  path: string
  connected: boolean
  reconnecting: boolean
  providers: DirectProvider[]
  banner: ReactNode
  sheet: ReactNode
  modelSheet: ReactNode
  onSelectHost: (fingerprint: string) => void
  onAddHost: () => void
  onUnlink: () => void
  onToggleLocale: () => void
  onModels: () => void
  onOpenChange: (open: boolean) => void
  onBindClose: (close: () => void) => void
}) {
  return (
    <div className="flex h-full min-w-0 flex-col overflow-hidden">
      <UpdateNotice />
      {banner}
      <div className="min-h-0 flex-1">
        <DirectChatScreen
          key={locale}
          hosts={hosts}
          activeFingerprint={activeFingerprint}
          path={path}
          connected={connected}
          reconnecting={reconnecting}
          providers={providers}
          onSelectHost={onSelectHost}
          onAddHost={onAddHost}
          onUnlink={onUnlink}
          onToggleLocale={onToggleLocale}
          onModels={onModels}
          onOpenChange={onOpenChange}
          onBindClose={onBindClose}
        />
      </div>
      {sheet}
      {modelSheet}
    </div>
  )
}
