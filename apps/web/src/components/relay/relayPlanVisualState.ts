const dateFormatter = new Intl.DateTimeFormat("en-US", {
  dateStyle: "medium",
  timeStyle: "short",
});

const relativeDateFormatter = new Intl.RelativeTimeFormat("en-US", {
  numeric: "auto",
});

export function formatPlanDate(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return "Unknown";
  }
  return dateFormatter.format(date);
}

export function formatPlanDateRelative(iso: string): string {
  const now = Date.now();
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "Unknown";
  }
  const diffSeconds = Math.round((then - now) / 1000);
  const diffMinutes = Math.round(diffSeconds / 60);
  const diffHours = Math.round(diffMinutes / 60);
  const diffDays = Math.round(diffHours / 24);
  if (Math.abs(diffSeconds) < 60) {
    return relativeDateFormatter.format(diffSeconds, "second");
  }
  if (Math.abs(diffMinutes) < 60) {
    return relativeDateFormatter.format(diffMinutes, "minute");
  }
  if (Math.abs(diffHours) < 24) {
    return relativeDateFormatter.format(diffHours, "hour");
  }
  return relativeDateFormatter.format(diffDays, "day");
}
