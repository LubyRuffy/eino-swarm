import { Badge } from "@/components/ui/badge"
import { skillIsStale, skillLastViewed } from "@/lib/skill-usage"
import type { SkillInfo } from "@/lib/types"
import { useT } from "@/lib/use-t"

export function SkillUsage({ skill, staleDays }: { skill: SkillInfo; staleDays?: number }) {
  const t = useT()
  const lastViewed = skillLastViewed(skill)
  return (
    <div data-testid="skill-usage" className="flex flex-wrap items-center gap-2 px-2 pb-1.5 text-xs text-muted-foreground">
      <span>{t("skill.views", { n: skill.views ?? 0 })}</span>
      {lastViewed === undefined ? (
        <span>{t("skill.neverViewed")}</span>
      ) : (
        <time dateTime={skill.last_viewed_at}>
          {t("skill.lastViewed", { when: new Date(lastViewed).toLocaleString(t.locale === "zh" ? "zh-CN" : "en-US") })}
        </time>
      )}
      {skillIsStale(skill, staleDays) ? (
        <Badge variant="warning">{t("skill.stale")}</Badge>
      ) : null}
    </div>
  )
}
