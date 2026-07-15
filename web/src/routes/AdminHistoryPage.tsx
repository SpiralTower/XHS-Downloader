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
        <span className="block max-w-80 truncate" title={item.requested_url}>
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
                ? "抓取"
                : item.source === "skipped"
                  ? "跳过"
                  : "待处理"}
          </Chip.Label>
        </Chip>
      </Table.Cell>
      <Table.Cell>
        {item.work ? (
          <div className="grid gap-0.5">
            <span>{item.work.platform_id}</span>
            <span className="text-xs text-muted">
              {item.version ? `v${item.version.number}` : "—"}
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
              ? `查看作品 ${item.work.platform_id}`
              : `记录 ${item.run_id} 无作品`
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
    <div className="grid gap-5">
      <header className="flex items-end justify-between gap-3">
        <h1
          className="text-2xl font-semibold tracking-[-0.03em] outline-none sm:text-3xl"
          id="main-heading"
          tabIndex={-1}
        >
          解析历史
        </h1>
        <Button
          isDisabled={isLoading}
          onPress={() => setReloadKey((current) => current + 1)}
          size="sm"
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
            <Alert.Description>{error}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}

      <Card className="glass-panel min-w-0 border border-border">
        <Card.Header className="flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-between">
          <Card.Title>请求记录</Card.Title>
          <TextField
            className="w-full sm:max-w-xs"
            isDisabled={!page}
            name="history_filter"
            onChange={setFilter}
            value={filter}
          >
            <Label className="sr-only">筛选</Label>
            <Input placeholder="筛选本页…" />
          </TextField>
        </Card.Header>

        <Card.Content className="min-w-0">
          {isLoading && !page ? (
            <div className="grid min-h-56 place-items-center">
              <Spinner color="accent" size="lg" />
            </div>
          ) : !page ? (
            <div className="grid min-h-56 place-items-center px-4 py-10 text-center">
              <div className="max-w-sm">
                <p className="font-medium">历史未载入</p>
                <Button
                  className="mt-4"
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
                    <Table.Column>链接</Table.Column>
                    <Table.Column>状态</Table.Column>
                    <Table.Column>来源</Table.Column>
                    <Table.Column>作品</Table.Column>
                    <Table.Column>详情</Table.Column>
                  </Table.Header>
                  <Table.Body
                    renderEmptyState={() => (
                      <div className="grid min-h-40 place-items-center px-4 py-10 text-center">
                        <div>
                          <SearchIcon className="mx-auto size-6 text-muted" />
                          <p className="mt-3 font-medium">
                            {filter ? "无匹配记录" : "暂无记录"}
                          </p>
                          {filter && (
                            <Button
                              className="mt-3"
                              onPress={() => setFilter("")}
                              size="sm"
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
                    第 {cursorStack.length + 1} 页 · {filteredItems.length} 条
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
    </div>
  );
}
