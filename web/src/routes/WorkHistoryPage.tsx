import {
  Alert,
  Button,
  Card,
  Chip,
  Link,
  Table,
  Tabs,
} from "@heroui/react";
import { useEffect, useMemo, useState } from "react";
import { useLoaderData, useNavigate } from "react-router";

import { formatDateTime, toWorkView } from "../api";
import {
  ArrowUpRightIcon,
  CheckIcon,
  ChevronRightIcon,
  ClockIcon,
  ImageIcon,
} from "../icons";
import type {
  ExtractionResource,
  SaveStatus,
  StoredVersion,
  WorkHistory,
} from "../types";

const saveLabels: Record<SaveStatus, string> = {
  disabled: "未保存",
  pending: "保存中",
  saved: "已保存",
  failed: "保存失败",
};

const saveColors: Record<
  SaveStatus,
  "default" | "warning" | "success" | "danger"
> = {
  disabled: "default",
  pending: "warning",
  saved: "success",
  failed: "danger",
};

function formatBytes(value?: number): string {
  if (value == null || !Number.isFinite(value)) return "—";
  return new Intl.NumberFormat("zh-CN", {
    notation: "compact",
    style: "unit",
    unit: "byte",
    unitDisplay: "narrow",
  }).format(value);
}

function ResourceTable({ resources }: { resources: ExtractionResource[] }) {
  return (
    <Table variant="secondary">
      <Table.ScrollContainer>
        <Table.Content
          aria-label="版本资源"
          className="min-w-[760px]"
        >
          <Table.Header>
            <Table.Column isRowHeader>资源</Table.Column>
            <Table.Column>保存状态</Table.Column>
            <Table.Column>类型 / 大小</Table.Column>
            <Table.Column>校验</Table.Column>
            <Table.Column>远端地址</Table.Column>
          </Table.Header>
          <Table.Body
            renderEmptyState={() => (
              <div className="grid min-h-48 place-items-center px-4 py-10 text-center">
                <div>
                  <ImageIcon className="mx-auto size-6 text-muted" />
                  <p className="mt-3 font-semibold">此版本没有资源</p>
                  <p className="mt-1 text-sm text-muted">
                    该快照只保存了作品元数据。
                  </p>
                </div>
              </div>
            )}
          >
            {resources.map((resource) => (
              <Table.Row id={resource.id} key={resource.id}>
                <Table.Cell>
                  <span className="font-medium">
                    {resource.kind === "text"
                      ? "文案"
                      : resource.kind === "image"
                        ? `图片 ${resource.ordinal}`
                        : `视频 ${resource.ordinal}`}
                  </span>
                </Table.Cell>
                <Table.Cell>
                  <div className="grid gap-1">
                    <Chip
                      color={saveColors[resource.save_status]}
                      size="sm"
                      variant="soft"
                    >
                      <Chip.Label>{saveLabels[resource.save_status]}</Chip.Label>
                    </Chip>
                    {resource.save_error && (
                      <span
                        className="max-w-48 truncate text-xs text-danger"
                        title={resource.save_error}
                      >
                        {resource.save_error}
                      </span>
                    )}
                  </div>
                </Table.Cell>
                <Table.Cell>
                  <div className="grid gap-0.5 text-xs">
                    <span>{resource.mime_type || "—"}</span>
                    <span className="text-muted">
                      {formatBytes(resource.size_bytes)}
                    </span>
                  </div>
                </Table.Cell>
                <Table.Cell>
                  <span
                    className="block max-w-32 truncate font-mono text-xs text-muted"
                    title={resource.sha256}
                  >
                    {resource.sha256 || "—"}
                  </span>
                </Table.Cell>
                <Table.Cell>
                  {resource.remote_url ? (
                    <Link
                      href={resource.remote_url}
                      rel="noreferrer"
                      target="_blank"
                    >
                      打开
                      <ArrowUpRightIcon className="size-4" />
                    </Link>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Content>
      </Table.ScrollContainer>
    </Table>
  );
}

function SnapshotPanel({ version }: { version: StoredVersion }) {
  const work = useMemo(
    () => toWorkView(version.data, version.resources),
    [version],
  );

  return (
    <div className="grid gap-5">
      <div className="flex flex-wrap items-center gap-2">
        <Chip color="accent" size="sm" variant="soft">
          <Chip.Label>{work.type}</Chip.Label>
        </Chip>
        <Chip size="sm" variant="tertiary">
          <Chip.Label>版本 v{version.number}</Chip.Label>
        </Chip>
      </div>

      <div>
        <h2 className="text-xl font-semibold">{work.title}</h2>
        <p className="mt-1 text-sm text-muted">
          {work.authorName} · 捕获于 {formatDateTime(version.captured_at)}
        </p>
      </div>

      <p className="whitespace-pre-wrap break-words text-sm leading-7 text-muted">
        {work.description}
      </p>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          ["点赞", work.metrics.liked],
          ["收藏", work.metrics.collected],
          ["评论", work.metrics.comments],
          ["分享", work.metrics.shared],
        ].map(([label, value]) => (
          <div className="rounded-xl bg-surface-secondary p-3" key={label}>
            <p className="text-lg font-semibold">{value}</p>
            <p className="text-xs text-muted">{label}</p>
          </div>
        ))}
      </div>

      {work.tags.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {work.tags.map((tag, index) => (
            <Chip key={`${tag}-${index}`} size="sm" variant="tertiary">
              <Chip.Label>#{tag}</Chip.Label>
            </Chip>
          ))}
        </div>
      )}

      {work.workUrl && (
        <Link href={work.workUrl} rel="noreferrer" target="_blank">
          查看原作品
          <ArrowUpRightIcon className="size-4" />
        </Link>
      )}
    </div>
  );
}

export default function WorkHistoryPage() {
  const history = useLoaderData() as WorkHistory;
  const navigate = useNavigate();
  const [selectedVersionID, setSelectedVersionID] = useState(
    history.versions[0]?.id ?? 0,
  );

  useEffect(() => {
    document.title = `作品 ${history.work.platform_id} · 版本历史`;
  }, [history.work.platform_id]);

  const selectedVersion =
    history.versions.find((version) => version.id === selectedVersionID) ??
    history.versions[0];

  return (
    <div className="grid gap-6">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <Button
            className="mb-4"
            onPress={() => navigate("/admin/history")}
            size="sm"
            variant="secondary"
          >
            返回解析历史
          </Button>
          <h1
            className="text-3xl font-bold tracking-[-0.035em] outline-none"
            id="main-heading"
            tabIndex={-1}
          >
            作品 {history.work.platform_id}
          </h1>
          <p className="mt-2 text-sm text-muted">
            首次发现 {formatDateTime(history.work.first_seen_at)} · 最近发现{" "}
            {formatDateTime(history.work.last_seen_at)}
          </p>
        </div>
        <Chip color="accent" variant="soft">
          <Chip.Label>{history.versions.length} 个版本</Chip.Label>
        </Chip>
      </header>

      {history.versions.length === 0 || !selectedVersion ? (
        <Alert status="warning">
          <Alert.Indicator>
            <ClockIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Title>暂无版本快照</Alert.Title>
            <Alert.Description>
              此作品记录存在，但还没有成功解析的内容版本。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : (
        <div className="grid items-start gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]">
          <Card className="glass-panel border border-border">
            <Card.Header>
              <div>
                <Card.Title>版本时间线</Card.Title>
                <Card.Description>新版本优先显示。</Card.Description>
              </div>
            </Card.Header>
            <Card.Content
              aria-label="选择作品版本"
              className="grid gap-2"
              role="group"
            >
              {history.versions.map((version) => {
                const selected = version.id === selectedVersion.id;
                return (
                  <Button
                    aria-pressed={selected}
                    className="h-auto w-full justify-between py-3 text-start"
                    key={version.id}
                    onPress={() => setSelectedVersionID(version.id)}
                    variant={selected ? "primary" : "secondary"}
                  >
                    <span className="grid gap-0.5">
                      <span className="font-medium">版本 v{version.number}</span>
                      <span
                        className={
                          selected
                            ? "text-accent-foreground/75 text-xs"
                            : "text-muted text-xs"
                        }
                      >
                        {formatDateTime(version.captured_at)}
                      </span>
                    </span>
                    <ChevronRightIcon className="size-4" />
                  </Button>
                );
              })}
            </Card.Content>
          </Card>

          <Card className="glass-panel min-w-0 border border-border">
            <Card.Content className="min-w-0 pt-5">
              <Tabs defaultSelectedKey="snapshot" variant="secondary">
                <Tabs.ListContainer>
                  <Tabs.List aria-label="版本详情">
                    <Tabs.Tab id="snapshot">
                      快照
                      <Tabs.Indicator />
                    </Tabs.Tab>
                    <Tabs.Tab id="resources">
                      资源（{selectedVersion.resources.length}）
                      <Tabs.Indicator />
                    </Tabs.Tab>
                  </Tabs.List>
                </Tabs.ListContainer>
                <Tabs.Panel className="pt-6" id="snapshot">
                  <SnapshotPanel version={selectedVersion} />
                </Tabs.Panel>
                <Tabs.Panel className="pt-6" id="resources">
                  <ResourceTable resources={selectedVersion.resources} />
                </Tabs.Panel>
              </Tabs>
            </Card.Content>
          </Card>
        </div>
      )}

      <p className="flex items-center gap-2 text-xs text-muted">
        <CheckIcon className="size-4 text-success" />
        浏览器仅接收脱敏资源元数据，不包含服务端文件路径。
      </p>
    </div>
  );
}
