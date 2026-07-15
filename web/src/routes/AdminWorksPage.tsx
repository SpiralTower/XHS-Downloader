import {
  Alert,
  Button,
  Card,
  Chip,
  Pagination,
  Spinner,
  Table,
} from "@heroui/react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router";

import { ApiError, formatDateTime, getWorks } from "../api";
import {
  EyeIcon,
  ImageIcon,
  RefreshIcon,
  XIcon,
} from "../icons";
import type { WorkListItem, WorkPage } from "../types";

const countFormatter = new Intl.NumberFormat("zh-CN");

function SavedWorkPreview({ work }: { work: WorkListItem }) {
  const [imageFailed, setImageFailed] = useState(false);
  const hasThumbnail = Boolean(work.thumbnail_url) && !imageFailed;

  return (
    <div className="flex min-w-64 items-center gap-3">
      <div className="grid size-14 shrink-0 place-items-center overflow-hidden rounded-xl border border-border bg-surface-secondary">
        {hasThumbnail ? (
          <img
            alt=""
            className="size-full object-cover"
            loading="lazy"
            onError={() => setImageFailed(true)}
            src={work.thumbnail_url}
          />
        ) : (
          <ImageIcon className="size-5 text-muted" />
        )}
      </div>
      <div className="min-w-0">
        <p
          className="max-w-72 truncate font-medium"
          title={work.title || "未保存标题"}
        >
          {work.title || "未保存标题"}
        </p>
        <p className="mt-0.5 text-xs text-muted">
          {hasThumbnail ? "已保存缩略图" : "未保存缩略图"}
        </p>
      </div>
    </div>
  );
}

export default function AdminWorksPage() {
  const navigate = useNavigate();
  const [page, setPage] = useState<WorkPage | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [cursorStack, setCursorStack] = useState<Array<string | null>>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  const load = useCallback(
    async (signal: AbortSignal) => {
      setIsLoading(true);
      setError("");
      try {
        setPage(await getWorks(cursor, 25, signal));
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        if (cause instanceof ApiError && cause.status === 401) {
          navigate("/admin/login?next=/admin/works", { replace: true });
          return;
        }
        setError(cause instanceof Error ? cause.message : "读取作品失败");
      } finally {
        if (!signal.aborted) setIsLoading(false);
      }
    },
    [cursor, navigate],
  );

  useEffect(() => {
    document.title = "作品 · XHS Downloader";
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load, reloadKey]);

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

  const row = (work: WorkListItem) => (
    <Table.Row id={work.id} key={work.id}>
      <Table.Cell>
        <span className="font-mono text-sm font-medium">
          {work.platform_id}
        </span>
      </Table.Cell>
      <Table.Cell>
        <SavedWorkPreview key={work.thumbnail_url || "none"} work={work} />
      </Table.Cell>
      <Table.Cell>
        <Chip color="accent" size="sm" variant="soft">
          <Chip.Label>{countFormatter.format(work.parse_count)}</Chip.Label>
        </Chip>
      </Table.Cell>
      <Table.Cell>
        <Chip size="sm" variant="tertiary">
          <Chip.Label>{countFormatter.format(work.version_count)}</Chip.Label>
        </Chip>
      </Table.Cell>
      <Table.Cell>
        <span className="whitespace-nowrap">
          {formatDateTime(work.last_parsed_at)}
        </span>
      </Table.Cell>
      <Table.Cell>
        <Button
          aria-label={"查看作品 " + work.platform_id}
          isIconOnly
          onPress={() => navigate("/admin/works/" + work.id)}
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
        <div>
          <h1
            className="text-2xl font-semibold tracking-[-0.03em] outline-none sm:text-3xl"
            id="main-heading"
            tabIndex={-1}
          >
            作品
          </h1>
          <p className="mt-1 text-sm text-muted">
            查看已经成功解析的小红书作品与保存内容
          </p>
        </div>
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
        <Card.Header>
          <Card.Title>已解析作品</Card.Title>
          <Card.Description>
            仅显示曾成功保存的标题与缩略图
          </Card.Description>
        </Card.Header>

        <Card.Content className="min-w-0">
          {isLoading && !page ? (
            <div className="grid min-h-56 place-items-center">
              <Spinner color="accent" size="lg" />
            </div>
          ) : !page ? (
            <div className="grid min-h-56 place-items-center px-4 py-10 text-center">
              <div className="max-w-sm">
                <p className="font-medium">作品未载入</p>
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
                  aria-label="已解析作品"
                  className="min-w-[980px]"
                >
                  <Table.Header>
                    <Table.Column isRowHeader>小红书 ID</Table.Column>
                    <Table.Column>已保存内容</Table.Column>
                    <Table.Column>解析次数</Table.Column>
                    <Table.Column>版本数</Table.Column>
                    <Table.Column>最近解析</Table.Column>
                    <Table.Column>详情</Table.Column>
                  </Table.Header>
                  <Table.Body
                    renderEmptyState={() => (
                      <div className="grid min-h-40 place-items-center px-4 py-10 text-center">
                        <div>
                          <ImageIcon className="mx-auto size-6 text-muted" />
                          <p className="mt-3 font-medium">暂无作品</p>
                          <p className="mt-1 text-sm text-muted">
                            成功解析后，作品会显示在这里
                          </p>
                        </div>
                      </div>
                    )}
                  >
                    {page.items.map(row)}
                  </Table.Body>
                </Table.Content>
              </Table.ScrollContainer>
              <Table.Footer>
                <Pagination className="w-full" size="sm">
                  <Pagination.Summary>
                    第 {cursorStack.length + 1} 页 · {page.items.length} 条
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
                        isDisabled={!page.next_cursor || isLoading}
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
