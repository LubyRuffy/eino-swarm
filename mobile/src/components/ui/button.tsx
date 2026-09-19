import type { ButtonHTMLAttributes } from "react"

import { cn } from "@/lib/cn"

export function Button({
  className,
  variant = "default",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "outline" | "ghost" | "destructive"
}) {
  return (
    <button
      className={cn(
        "inline-flex h-10 items-center justify-center rounded-md px-4 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
        variant === "default" && "bg-primary text-primary-foreground",
        variant === "outline" && "border border-input bg-background",
        variant === "ghost" && "hover:bg-accent",
        variant === "destructive" && "bg-destructive text-destructive-foreground",
        className,
      )}
      {...props}
    />
  )
}
