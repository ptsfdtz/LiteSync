import { cn } from "@workspace/ui/lib/utils"

type ProgressProps = {
  value: number
  className?: string
}

function Progress({ value, className }: ProgressProps) {
  const safeValue = Number.isFinite(value)
    ? Math.min(Math.max(value, 0), 100)
    : 0

  return (
    <div
      data-slot="progress"
      className={cn(
        "relative h-2 w-full overflow-hidden rounded-full bg-muted",
        className
      )}
    >
      <div
        className="h-full bg-primary transition-[width] duration-300 ease-out"
        style={{ width: `${safeValue}%` }}
      />
    </div>
  )
}

export { Progress }
