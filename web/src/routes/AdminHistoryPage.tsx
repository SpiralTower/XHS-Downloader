import {
  Alert,
  Button,
  Card,
  Chip,
  Input,
  Label,
  Pagination,
  Spinner,
  Table,
  TextField,
} from "@heroui/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";

import { ApiError, formatDateTime, getHistory } from "../api";
import {
  ClockIcon,
  EyeIcon,
  RefreshIcon,
  SearchIcon,
  XIcon,
} from "../icons";
import type {
  HistoryItem,
  HistoryPage,
  HistoryStatus,
} from "../types";

const statusLabels: Record<HistoryStatus, string> = {
  running: "进行中",
  succeeded: "成功",
  failed: "失败",
};

const statusColors: Record<
  HistoryStatus,
  "default" | "success" | "danger"
> = {
  running: "default",
  succeeded: "success",
  failed: "danger",
};

export default function AdminHistoryPage() {
  const navigate = useNavigate();
  const [page, setPage] = useState<HistoryPage | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [cursorStack, setCursorStack] = useState<Array<string | null>>([]);
  const [filter, setFilter] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  const load = useCallback(
    async (signal: AbortSignal) => {
      setIsLoading(true);
      setError("");
      try {
        setPage(await getHistory(cursor, 25, signal));
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        if (cause instanceof ApiError && cause.status === 401) {
          navigate("/admin/login?next=/admin/history", { replace: true });
          return;
        }
        setError(cause instanceof Error ? cause.message : "读取解析历史失败");
      } finally {
        if (!signal.aborted) setIsLoading(false);
      }
    },
    [cursor, navigate],
  );

  useEffect(() => {
    document.title = "解析历史 · XHS Downloader";
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load, reloadKey]);

  const filteredItems = useMemo(() => {
    const query = filter.trim().toLocaleLowerCase();
    if (!query) return page?.items ?? [];
    return (page?.items ?? []).filter((item) => {
      const source = [
        item.requested_url,
        item.work?.platform_id,
        item.status,
        item.source,
        item.error,
        String(item.run_id),
      ]
        .filter(Boolean)
        .join(" ")
        .toLocaleLowerCase();
      return source.includes(query);
    });
  }, [filter, page]);

  const goNext = () => {
    if (!page?.next_cursor) return;
    setCursorStack((current) => [...current, cursor]);
    setCursor(page.next_cursor);
  };

  const goPrevious = () => {
    setCursorStack((current) => {
      if (current.length === 0) return current;
      const nextStack = [...current];
      setCursor(nextStack.pop() ?? null);
      return nextStack;
    });
  };

  const row = (item: HistoryItem) => (
    <Table.Row id={item.run_id} key={item.run_id}>
      <Table.Cell>
        <div className="grid gap-0.5">
          <span className="font-medium">#{item.run_id}</span>
          <span className="text-xs text-muted">
            {formatDateTime(item.started_at)}
          </span>
        </div>
      </Table.Cell>
      <Table.Cell>
        <span
          className="block max-w-80 truncate"
          title={item.requested_url}
        >
          {item.requested_url}
        </span>
      </Table.Cell>
      <Table.Cell>
        <Chip color={statusColors[item.status]} size="sm" variant="soft">
          <Chip.Label>{statusLabels[item.status]}</Chip.Label>
        </Chip>
      </Table.Cell>
      <Table.Cell>
        <Chip size="sm" variant="tertiary">
          <Chip.Label>
            {item.source === "cache"
              ? "缓存"
              : item.source === "fetched"
                ? "重新抓取"
                : item.source === "skipped"
                  ? "旧版跳过"
                  : "待处理"}
          </Chip.Label>
        </Chip>
      </Table.Cell>
      <Table.Cell>
        {item.work ? (
          <div className="grid gap-0.5">
            <span>{item.work.platform_id}</span>
            <span className="text-xs text-muted">
              {item.version ? `版本 v${item.version.number}` : "未生成版本"}
            </span>
          </div>
        ) : (
          <span className="text-muted">—</span>
        )}
      </Table.Cell>
      <Table.Cell>
        <Button
          aria-label={
            item.work
              ? `查看作品 ${item.work.platform_id} 的版本历史`
              : `记录 ${item.run_id} 尚无作品详情`
          }
          isDisabled={!item.work}
          isIconOnly
          onPress={() =>
            item.work && navigate(`/admin/history/${item.work.id}`)
          }
          size="sm"
          variant="secondary"
        >
          <EyeIcon className="size-4" />
        </Button>
      </Table.Cell>
    </Table.Row>
  );

  return (
    <div className="grid gap-6">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1
            className="text-3xl font-bold tracking-[-0.035em] outline-none"
            id="main-heading"
            tabIndex={-1}
          >
            解析历史
          </h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-muted">
            每次请求都保留独立 run，并关联作品及当时使用的内容版本。
          </p>
        </div>
        <Button
          isDisabled={isLoading}
          onPress={() => setReloadKey((current) => current + 1)}
          variant="secondary"
        >
          <RefreshIcon className="size-4" />
          刷新
        </Button>
      </header>

      {error && (
        <Alert status="danger">
          <Alert.Indicator>
            <XIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Title>无法读取历史记录</Alert.Title>
            <Alert.Description>{error}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}

      <Card className="glass-panel min-w-0 border border-border">
        <Card.Header className="flex-col items-stretch gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <Card.Title>请求记录</Card.Title>
            <Card.Description>
              当前筛选仅作用于本页；服务端按最新 run 倒序返回。
            </Card.Description>
          </div>
          <TextField
            className="w-full sm:max-w-sm"
            isDisabled={!page}
            name="history_filter"
            onChange={setFilter}
            value={filter}
          >
            <Label>筛选当前页</Label>
            <Input placeholder="链接、作品 ID、状态或 run ID" />
          </TextField>
        </Card.Header>

        <Card.Content className="min-w-0">
          {isLoading && !page ? (
            <div className="grid min-h-64 place-items-center text-center">
              <div role="status">
                <Spinner color="accent" size="lg" />
                <p className="mt-4 font-medium">正在读取解析历史…</p>
              </div>
            </div>
          ) : !page ? (
            <div className="grid min-h-64 place-items-center px-4 py-10 text-center">
              <div className="max-w-md">
                <XIcon className="mx-auto size-7 text-danger" />
                <h2 className="mt-4 text-lg font-semibold">历史记录尚未载入</h2>
                <p className="mt-2 text-sm leading-6 text-muted">
                  本次请求失败，当前状态不代表历史记录为空。请重新载入后再查看。
                </p>
                <Button
                  className="mt-5"
                  onPress={() => setReloadKey((current) => current + 1)}
                  variant="secondary"
                >
                  <RefreshIcon className="size-4" />
                  重新载入
                </Button>
              </div>
            </div>
          ) : (
            <Table variant="secondary">
              <Table.ScrollContainer>
                <Table.Content
                  aria-label="解析历史记录"
                  className="min-w-[920px]"
                >
                  <Table.Header>
                    <Table.Column isRowHeader>记录</Table.Column>
                    <Table.Column>提交链接</Table.Column>
                    <Table.Column>状态</Table.Column>
                    <Table.Column>来源</Table.Column>
                    <Table.Column>作品 / 版本</Table.Column>
                    <Table.Column>详情</Table.Column>
                  </Table.Header>
                  <Table.Body
                    renderEmptyState={() => (
                      <div className="grid min-h-48 place-items-center px-4 py-10 text-center">
                        <div>
                          <SearchIcon className="mx-auto size-6 text-muted" />
                          <p className="mt-3 font-semibold">
                            {filter ? "本页没有匹配记录" : "还没有解析记录"}
                          </p>
                          <p className="mt-1 text-sm text-muted">
                            {filter
                              ? "清除筛选词后查看本页全部记录。"
                              : "用户端完成第一次解析后，记录会显示在这里。"}
                          </p>
                          {filter && (
                            <Button
                              className="mt-4"
                              onPress={() => setFilter("")}
                              variant="secondary"
                            >
                              清除筛选
                            </Button>
                          )}
                        </div>
                      </div>
                    )}
                  >
                    {filteredItems.map(row)}
                  </Table.Body>
                </Table.Content>
              </Table.ScrollContainer>
              <Table.Footer>
                <Pagination className="w-full" size="sm">
                  <Pagination.Summary>
                    第 {cursorStack.length + 1} 页 · 本页 {filteredItems.length} 条
                  </Pagination.Summary>
                  <Pagination.Content>
                    <Pagination.Item>
                      <Pagination.Previous
                        isDisabled={cursorStack.length === 0 || isLoading}
                        onPress={goPrevious}
                      >
                        <Pagination.PreviousIcon />
                        <span>上一页</span>
                      </Pagination.Previous>
                    </Pagination.Item>
                    <Pagination.Item>
                      <Pagination.Link isActive>
                        {cursorStack.length + 1}
                      </Pagination.Link>
                    </Pagination.Item>
                    <Pagination.Item>
                      <Pagination.Next
                        isDisabled={!page?.next_cursor || isLoading}
                        onPress={goNext}
                      >
                        <span>下一页</span>
                        <Pagination.NextIcon />
                      </Pagination.Next>
                    </Pagination.Item>
                  </Pagination.Content>
                </Pagination>
              </Table.Footer>
            </Table>
          )}
        </Card.Content>
      </Card>

      <p className="flex items-center gap-2 text-xs text-muted">
        <ClockIcon className="size-4" />
        时间按浏览器所在时区显示；历史数据由 SQLite 持久化。
      </p>
    </div>
  );
}
