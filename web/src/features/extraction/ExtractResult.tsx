import { Card, Chip, Link, Tag, TagGroup } from "@heroui/react";
import { useMemo } from "react";

import { formatDateTime, toWorkView } from "../../api";
import {
  ArrowUpRightIcon,
  ClockIcon,
  DownloadIcon,
  ImageIcon,
  RefreshIcon,
} from "../../icons";
import type { ExtractionResponse } from "../../types";
import MediaPreview from "./MediaPreview";

const metricLabels = {
  liked: "点赞",
  collected: "收藏",
  comments: "评论",
  shared: "分享",
} as const;

const connectionLabels = {
  default: "默认",
  override: "覆盖",
  disabled: "禁用",
  none: "未用",
} as const;

export default function ExtractResult({
  response,
}: {
  response: ExtractionResponse;
}) {
  const work = useMemo(
    () => toWorkView(response.data, response.version.resources),
    [response],
  );

  return (
    <section
      aria-label="解析结果"
      className="grid min-w-0 gap-4"
      tabIndex={-1}
    >
      <Card className="glass-panel min-w-0 border border-border">
        <Card.Header className="min-w-0 items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <Card.Title className="break-words text-xl [overflow-wrap:anywhere]">
              {work.title}
            </Card.Title>
            <div className="mt-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
              {work.authorUrl ? (
                <Link
                  className="inline-flex min-w-0 max-w-full items-center gap-1"
                  href={work.authorUrl}
                  rel="noreferrer"
                  target="_blank"
                >
                  <span className="truncate">{work.authorName}</span>
                  <ArrowUpRightIcon className="size-3.5 shrink-0" />
                </Link>
              ) : (
                <span>{work.authorName}</span>
              )}
              <span aria-hidden="true">·</span>
              <span>{work.publishedAt}</span>
            </div>
          </div>
          <Chip color="accent" size="sm" variant="soft">
            <Chip.Label>{work.type}</Chip.Label>
          </Chip>
        </Card.Header>

        <Card.Content className="grid min-w-0 gap-4">
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
            <span className="inline-flex items-center gap-1.5">
              {response.source === "cache" ? (
                <ClockIcon className="size-3.5" />
              ) : (
                <RefreshIcon className="size-3.5" />
              )}
              {response.source === "cache" ? "缓存" : "新抓取"}
            </span>
            <span>
              v{response.version.number} ·{" "}
              {formatDateTime(response.version.captured_at)}
            </span>
            <span>
              Cookie {connectionLabels[response.connection.cookie_source]}
            </span>
            <span>
              代理 {connectionLabels[response.connection.proxy_source]}
            </span>
          </div>

          {work.description && (
            <p className="whitespace-pre-wrap break-words text-sm leading-7 text-muted [overflow-wrap:anywhere]">
              {work.description}
            </p>
          )}

          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {Object.entries(work.metrics).map(([key, value]) => (
              <div
                className="min-w-0 rounded-xl bg-surface-secondary p-3"
                key={key}
              >
                <p className="truncate text-lg font-semibold">{value}</p>
                <p className="text-xs text-muted">
                  {metricLabels[key as keyof typeof metricLabels]}
                </p>
              </div>
            ))}
          </div>

          {work.tags.length > 0 && (
            <TagGroup aria-label="标签" size="sm">
              <TagGroup.List>
                {work.tags.map((tag, index) => (
                  <Tag
                    className="max-w-full truncate"
                    id={`${tag}-${index}`}
                    key={`${tag}-${index}`}
                  >
                    #{tag}
                  </Tag>
                ))}
              </TagGroup.List>
            </TagGroup>
          )}

          {work.media.length > 0 && (
            <div className="grid gap-3">
              <div className="flex items-center justify-between gap-3">
                <h2 className="text-sm font-semibold">媒体</h2>
                <span className="text-xs text-muted">{work.media.length}</span>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                {work.media.slice(0, 4).map((item, index) => (
                  <MediaPreview
                    item={item}
                    key={`${item.url}-${index}`}
                    title={work.title}
                  />
                ))}
              </div>
              <div className="media-scroll grid max-h-56 gap-1.5 overflow-y-auto pr-1">
                {work.media.map((item, index) => (
                  <Link
                    className="flex min-w-0 items-center justify-between rounded-xl border border-border px-3 py-2 text-sm"
                    href={item.url}
                    key={`${item.url}-download-${index}`}
                    rel="noreferrer"
                    target="_blank"
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <ImageIcon className="size-4 shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </span>
                    <DownloadIcon className="size-4 shrink-0" />
                  </Link>
                ))}
              </div>
            </div>
          )}
        </Card.Content>

        <Card.Footer className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <span className="max-w-full truncate text-xs text-muted">
            {response.work.platform_id} · #{response.run_id}
          </span>
          {work.workUrl && (
            <Link href={work.workUrl} rel="noreferrer" target="_blank">
              原作品
              <ArrowUpRightIcon className="size-4" />
            </Link>
          )}
        </Card.Footer>
      </Card>
    </section>
  );
}
