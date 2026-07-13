import { Chip } from "@heroui/react";

import { ArrowUpRightIcon } from "../../icons";
import type { MediaItem, SaveStatus } from "../../types";

const saveLabels: Record<SaveStatus, string> = {
  disabled: "未保存",
  pending: "保存中",
  saved: "已保存",
  failed: "保存失败",
};

export default function MediaPreview({
  item,
  title,
}: {
  item: MediaItem;
  title: string;
}) {
  const accessibleLabel = `${title} · ${item.label}`;

  return (
    <figure className="overflow-hidden rounded-2xl border border-border bg-surface-secondary">
      {item.kind === "image" ? (
        <a
          aria-label={`打开${accessibleLabel}`}
          href={item.url}
          rel="noreferrer"
          target="_blank"
        >
          <img
            alt={accessibleLabel}
            className="aspect-video w-full bg-surface object-cover"
            loading="lazy"
            referrerPolicy="no-referrer"
            src={item.url}
          />
        </a>
      ) : (
        <video
          aria-label={accessibleLabel}
          className="aspect-video w-full bg-black object-contain"
          controls
          playsInline
          preload="metadata"
          src={item.url}
        />
      )}
      <figcaption className="flex min-w-0 items-center justify-between gap-2 px-3 py-2 text-xs">
        <span className="min-w-0 truncate text-muted">{item.label}</span>
        <span className="flex shrink-0 items-center gap-2">
          {item.saveStatus && (
            <Chip
              color={item.saveStatus === "failed" ? "danger" : "default"}
              size="sm"
              variant="soft"
            >
              <Chip.Label>{saveLabels[item.saveStatus]}</Chip.Label>
            </Chip>
          )}
          <a
            aria-label={`在新窗口打开${item.label}`}
            className="rounded text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            href={item.url}
            rel="noreferrer"
            target="_blank"
          >
            <ArrowUpRightIcon className="size-4" />
          </a>
        </span>
      </figcaption>
    </figure>
  );
}
