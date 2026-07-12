import {
  Alert,
  Button,
  Card,
  Checkbox,
  Chip,
  Description,
  FieldError,
  Input,
  Label,
  Link,
  Spinner,
  TextField,
} from "@heroui/react";
import { useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, RefObject } from "react";

import {
  ApiError,
  checkHealth,
  extractDetail,
  getDownloadError,
  getSkipMessage,
  parseIndexes,
  toWorkView,
} from "./api";
import {
  ActivityIcon,
  ArrowUpRightIcon,
  CheckIcon,
  DownloadIcon,
  ImageIcon,
  MoonIcon,
  SearchIcon,
  ShieldIcon,
  SparklesIcon,
  SunIcon,
  XIcon,
} from "./icons";
import {
  applyTheme,
  readThemePreference,
  storeThemePreference,
} from "./theme";
import type { ThemePreference } from "./theme";
import type {
  ExtractResponse,
  FormValues,
  HealthSnapshot,
  MediaItem,
  RequestState,
} from "./types";

const initialValues: FormValues = {
  url: "",
  download: false,
  indexes: "",
  cookie: "",
  proxy: "",
  skip: false,
};

const metricLabels = {
  liked: "点赞",
  collected: "收藏",
  comments: "评论",
  shared: "分享",
} as const;

function Choice({
  selected,
  onChange,
  title,
  description,
}: {
  selected: boolean;
  onChange: (value: boolean) => void;
  title: string;
  description: string;
}) {
  return (
    <Checkbox
      className="rounded-2xl border border-zinc-200/80 bg-zinc-50/80 p-3.5 dark:border-white/10 dark:bg-white/[0.03]"
      isSelected={selected}
      onChange={onChange}
      variant="secondary"
    >
      <Checkbox.Content className="flex items-start gap-3">
        <Checkbox.Control className="mt-0.5">
          <Checkbox.Indicator>
            <CheckIcon className="size-3.5" />
          </Checkbox.Indicator>
        </Checkbox.Control>
        <span className="grid gap-0.5">
          <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">
            {title}
          </span>
          <span className="text-xs leading-5 text-zinc-500 dark:text-zinc-400">
            {description}
          </span>
        </span>
      </Checkbox.Content>
    </Checkbox>
  );
}

function MediaPreview({
  item,
  title,
}: {
  item: MediaItem;
  title: string;
}) {
  const accessibleLabel = title + " · " + item.label;

  return (
    <figure className="overflow-hidden rounded-2xl border border-zinc-200/80 bg-zinc-100 dark:border-white/10 dark:bg-black/20">
      {item.kind === "image" ? (
        <a
          aria-label={"打开" + accessibleLabel}
          href={item.url}
          rel="noreferrer"
          target="_blank"
        >
          <img
            alt={accessibleLabel}
            className="aspect-video w-full bg-zinc-200 object-cover dark:bg-zinc-900"
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
        <span className="truncate text-zinc-600 dark:text-zinc-300">
          {item.label}
        </span>
        <a
          aria-label={"在新窗口打开" + item.label}
          className="shrink-0 text-rose-600 hover:text-rose-700 dark:text-rose-400"
          href={item.url}
          rel="noreferrer"
          target="_blank"
        >
          <ArrowUpRightIcon className="size-4" />
        </a>
      </figcaption>
    </figure>
  );
}

function focusInput(ref: RefObject<HTMLInputElement | null>): void {
  window.requestAnimationFrame(() => ref.current?.focus());
}

const supportedLinkPattern =
  /(?:https?:\/\/)?(?:www\.)?(?:(?:xiaohongshu|rednote)\.com\/(?:explore\/\S+|discovery\/item\/\S+|user\/profile\/[a-z0-9]+\/\S+)|xhslink\.com\/\S+)/i;

function validateWorkUrl(value: string): string {
  const candidate = value.match(supportedLinkPattern)?.[0];
  if (!candidate) {
    return "请输入有效的小红书、RedNote 或 xhslink 作品链接";
  }

  try {
    const parsed = new URL(
      /^https?:\/\//i.test(candidate) ? candidate : "https://" + candidate,
    );
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "作品链接必须使用 http 或 https";
    }
  } catch {
    return "请输入完整、有效的作品链接";
  }
  return "";
}

export default function App() {
  const [values, setValues] = useState(initialValues);
  const [state, setState] = useState<RequestState>("idle");
  const [response, setResponse] = useState<ExtractResponse | null>(null);
  const [error, setError] = useState("");
  const [urlError, setUrlError] = useState("");
  const [indexError, setIndexError] = useState("");
  const [theme, setTheme] = useState<ThemePreference>(readThemePreference);
  const [health, setHealth] = useState<HealthSnapshot>({
    state: "checking",
    latency: null,
    checkedAt: null,
  });
  const controllerRef = useRef<AbortController | null>(null);
  const urlInputRef = useRef<HTMLInputElement>(null);
  const indexInputRef = useRef<HTMLInputElement>(null);
  const resultRegionRef = useRef<HTMLElement>(null);

  const skipMessage = useMemo(
    () => (response ? getSkipMessage(response) : null),
    [response],
  );
  const work = useMemo(
    () =>
      response?.data && !skipMessage ? toWorkView(response.data) : null,
    [response, skipMessage],
  );

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const syncTheme = () => applyTheme(theme);

    storeThemePreference(theme);
    syncTheme();
    if (theme !== "system") return;

    media.addEventListener("change", syncTheme);
    return () => media.removeEventListener("change", syncTheme);
  }, [theme]);

  useEffect(() => {
    let healthController: AbortController | null = null;

    const runHealthCheck = async () => {
      healthController?.abort();
      healthController = new AbortController();
      setHealth((current) => ({ ...current, state: "checking" }));

      try {
        const latency = await checkHealth(healthController.signal);
        setHealth({ state: "online", latency, checkedAt: new Date() });
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        setHealth({ state: "offline", latency: null, checkedAt: new Date() });
      }
    };

    void runHealthCheck();
    const timer = window.setInterval(() => void runHealthCheck(), 30_000);
    return () => {
      window.clearInterval(timer);
      healthController?.abort();
    };
  }, []);

  useEffect(() => {
    if (state === "error" || state === "warning" || state === "skipped") {
      resultRegionRef.current?.focus();
    }
  }, [state]);

  const patchValues = (patch: Partial<FormValues>) =>
    setValues((current) => ({ ...current, ...patch }));

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setUrlError("");
    setIndexError("");
    setError("");

    const submittedUrl = values.url.trim();
    if (!submittedUrl) {
      setResponse(null);
      setState("idle");
      setUrlError("请输入小红书或 RedNote 作品链接");
      focusInput(urlInputRef);
      return;
    }

    const invalidUrl = validateWorkUrl(submittedUrl);
    if (invalidUrl) {
      setResponse(null);
      setState("idle");
      setUrlError(invalidUrl);
      focusInput(urlInputRef);
      return;
    }

    let indexes: number[] | null = null;
    if (values.download) {
      try {
        indexes = parseIndexes(values.indexes);
      } catch (cause) {
        setResponse(null);
        setState("idle");
        setIndexError(
          cause instanceof Error ? cause.message : "图片序号格式错误",
        );
        focusInput(indexInputRef);
        return;
      }
    }

    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setState("loading");
    setResponse(null);

    try {
      const result = await extractDetail(
        {
          url: submittedUrl,
          download: values.download,
          index: indexes,
          cookie: values.cookie.trim() || null,
          proxy: values.proxy.trim() || null,
          skip: values.skip,
        },
        controller.signal,
      );
      setResponse(result);
      if (!result.data) throw new ApiError(result.message);

      if (getSkipMessage(result)) {
        setState("skipped");
        return;
      }

      setState(getDownloadError(result.data) ? "warning" : "success");
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === "AbortError") return;
      setError(cause instanceof Error ? cause.message : "请求失败，请稍后重试");
      setState("error");
    }
  };

  const reset = () => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    setValues(initialValues);
    setResponse(null);
    setError("");
    setUrlError("");
    setIndexError("");
    setState("idle");
    focusInput(urlInputRef);
  };

  const healthLabel =
    health.state === "checking"
      ? "正在检查 API"
      : health.state === "online"
        ? "API 在线 · " + health.latency + "ms"
        : "API 离线";
  const healthColor =
    health.state === "online"
      ? "success"
      : health.state === "offline"
        ? "danger"
        : "default";
  const healthTitle = health.checkedAt
    ? "上次检查：" + health.checkedAt.toLocaleTimeString("zh-CN")
    : "正在执行首次健康检查";
  const liveStatus =
    state === "loading"
      ? "正在获取作品数据"
      : state === "success"
        ? "作品提取完成"
        : state === "warning"
          ? "作品信息已提取，但媒体下载失败"
          : state === "skipped"
            ? "作品已跳过"
            : state === "error"
              ? "提取失败：" + error
              : "";

  return (
    <div className="app-shell relative min-h-screen overflow-hidden bg-background text-foreground">
      <div className="surface-grid pointer-events-none absolute inset-0" />
      <main className="relative mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8 lg:py-12">
        <header className="mb-8 flex flex-col gap-5 sm:flex-row sm:items-start sm:justify-between">
          <div className="max-w-2xl">
            <Chip color="accent" size="sm" variant="soft">
              <Chip.Label>API Console · HeroUI v3</Chip.Label>
            </Chip>
            <h1 className="mt-4 text-balance text-3xl font-bold tracking-[-0.04em] sm:text-5xl">
              把作品链接，变成可用的数据与媒体。
            </h1>
            <p className="mt-3 max-w-xl text-sm leading-6 text-zinc-600 dark:text-zinc-400 sm:text-base">
              一个专注、轻量的 XHS Downloader Web 控制台。
            </p>
          </div>

          <div className="flex flex-col items-start gap-3 sm:items-end">
            <Chip
              color={healthColor}
              size="sm"
              title={healthTitle}
              variant="soft"
            >
              <Chip.Label className="flex items-center gap-1.5">
                <ActivityIcon className="size-3.5" />
                {healthLabel}
              </Chip.Label>
            </Chip>
            <div
              aria-label="外观主题"
              className="flex flex-wrap gap-1 rounded-xl border border-zinc-200/80 bg-white/70 p-1 dark:border-white/10 dark:bg-white/[0.04]"
              role="group"
            >
              <Button
                aria-pressed={theme === "light"}
                onPress={() => setTheme("light")}
                size="sm"
                type="button"
                variant={theme === "light" ? "primary" : "secondary"}
              >
                <SunIcon className="size-4" />
                浅色
              </Button>
              <Button
                aria-pressed={theme === "dark"}
                onPress={() => setTheme("dark")}
                size="sm"
                type="button"
                variant={theme === "dark" ? "primary" : "secondary"}
              >
                <MoonIcon className="size-4" />
                深色
              </Button>
              <Button
                aria-pressed={theme === "system"}
                onPress={() => setTheme("system")}
                size="sm"
                type="button"
                variant={theme === "system" ? "primary" : "secondary"}
              >
                <SparklesIcon className="size-4" />
                系统
              </Button>
            </div>
            <div className="flex items-center gap-2 text-xs text-zinc-500 dark:text-zinc-400">
              <ShieldIcon className="size-4 text-emerald-500" />
              Cookie 与代理仅保留在本次页面会话中
            </div>
          </div>
        </header>

        <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,0.82fr)_minmax(0,1.18fr)]">
          <Card
            className="glass-panel border border-white/80 dark:border-white/10"
            variant="default"
          >
            <Card.Header>
              <div>
                <Card.Title>提取作品</Card.Title>
                <Card.Description>填写链接，按需附带下载选项。</Card.Description>
              </div>
            </Card.Header>
            <form noValidate onSubmit={submit}>
              <Card.Content className="grid gap-5">
                <TextField
                  fullWidth
                  isInvalid={Boolean(urlError)}
                  isRequired
                  onChange={(url) => {
                    patchValues({ url });
                    setUrlError("");
                  }}
                  value={values.url}
                >
                  <Label>作品链接</Label>
                  <Input
                    autoCapitalize="none"
                    autoComplete="url"
                    inputMode="url"
                    name="url"
                    placeholder="https://www.xiaohongshu.com/explore/..."
                    ref={urlInputRef}
                    spellCheck={false}
                    type="url"
                  />
                  <Description>
                    支持小红书、RedNote 与 xhslink 分享链接。
                  </Description>
                  {urlError && <FieldError>{urlError}</FieldError>}
                </TextField>

                <div className="grid gap-3 sm:grid-cols-2">
                  <Choice
                    description="让服务端同步保存作品文件"
                    onChange={(download) => {
                      patchValues({ download });
                      if (!download) setIndexError("");
                    }}
                    selected={values.download}
                    title="下载媒体"
                  />
                  <Choice
                    description="已有下载记录时不再返回数据"
                    onChange={(skip) => patchValues({ skip })}
                    selected={values.skip}
                    title="跳过已下载"
                  />
                </div>

                <TextField
                  fullWidth
                  isDisabled={!values.download}
                  isInvalid={Boolean(indexError)}
                  onChange={(indexes) => {
                    patchValues({ indexes });
                    setIndexError("");
                  }}
                  value={values.indexes}
                >
                  <Label>图片序号</Label>
                  <Input
                    name="indexes"
                    placeholder="1, 3, 5-7"
                    ref={indexInputRef}
                    spellCheck={false}
                  />
                  <Description>
                    可选；仅下载媒体时生效，留空表示下载全部。
                  </Description>
                  {indexError && <FieldError>{indexError}</FieldError>}
                </TextField>

                <details className="group rounded-2xl border border-zinc-200/80 bg-zinc-50/60 p-4 dark:border-white/10 dark:bg-white/[0.025]">
                  <summary className="cursor-pointer list-none text-sm font-semibold">
                    高级连接选项
                  </summary>
                  <div className="mt-4 grid gap-4">
                    <TextField
                      fullWidth
                      onChange={(cookie) => patchValues({ cookie })}
                      value={values.cookie}
                    >
                      <Label>Cookie</Label>
                      <Input
                        autoComplete="off"
                        name="cookie"
                        placeholder="可选"
                        spellCheck={false}
                        type="password"
                      />
                      <Description>
                        不会写入 localStorage 或其他持久化存储。
                      </Description>
                    </TextField>
                    <TextField
                      fullWidth
                      onChange={(proxy) => patchValues({ proxy })}
                      value={values.proxy}
                    >
                      <Label>代理地址（隐藏输入）</Label>
                      <Input
                        autoComplete="off"
                        name="proxy"
                        placeholder="https://user:password@proxy.example.com:8443"
                        spellCheck={false}
                        type="password"
                      />
                      <Description>
                        默认仅允许公网代理；私网代理需服务端显式开启。
                      </Description>
                    </TextField>
                  </div>
                </details>
              </Card.Content>
              <Card.Footer className="flex gap-3">
                <Button
                  fullWidth
                  isDisabled={state === "loading"}
                  type="submit"
                  variant="primary"
                >
                  {state === "loading" ? (
                    <Spinner color="current" size="sm" />
                  ) : (
                    <SearchIcon className="size-4" />
                  )}
                  {state === "loading" ? "正在提取…" : "开始提取"}
                </Button>
                <Button onPress={reset} type="button" variant="secondary">
                  重置
                </Button>
              </Card.Footer>
            </form>
          </Card>

          <p
            aria-atomic="true"
            aria-live={state === "error" ? "assertive" : "polite"}
            className="sr-only"
          >
            {liveStatus}
          </p>
          <section
            aria-label="提取结果"
            className="grid gap-4 outline-none"
            ref={resultRegionRef}
            tabIndex={-1}
          >
            {state === "idle" && (
              <Card
                className="glass-panel min-h-72 border border-dashed border-zinc-300 dark:border-white/15"
                variant="transparent"
              >
                <Card.Content className="grid min-h-72 place-items-center text-center">
                  <div className="max-w-sm">
                    <span className="mx-auto grid size-14 place-items-center rounded-2xl bg-rose-500/10 text-rose-500">
                      <SparklesIcon className="size-7" />
                    </span>
                    <h2 className="mt-4 font-semibold">等待一次提取任务</h2>
                    <p className="mt-2 text-sm leading-6 text-zinc-500">
                      结果会在这里展示，包括作品信息、互动数据和媒体地址。
                    </p>
                  </div>
                </Card.Content>
              </Card>
            )}

            {state === "loading" && (
              <Card className="glass-panel min-h-72 border border-white/80 dark:border-white/10">
                <Card.Content className="grid min-h-72 place-items-center text-center">
                  <div>
                    <Spinner color="accent" size="lg" />
                    <p className="mt-4 font-medium">正在获取作品数据…</p>
                  </div>
                </Card.Content>
              </Card>
            )}

            {state === "error" && (
              <Alert status="danger">
                <Alert.Indicator>
                  <XIcon className="size-5" />
                </Alert.Indicator>
                <Alert.Content>
                  <Alert.Title>提取失败</Alert.Title>
                  <Alert.Description>{error}</Alert.Description>
                </Alert.Content>
              </Alert>
            )}

            {state === "skipped" && skipMessage && (
              <Alert status="warning">
                <Alert.Indicator>
                  <CheckIcon className="size-5" />
                </Alert.Indicator>
                <Alert.Content>
                  <Alert.Title>已跳过</Alert.Title>
                  <Alert.Description>{skipMessage}</Alert.Description>
                </Alert.Content>
              </Alert>
            )}

            {state === "success" && response && (
              <Alert status="success">
                <Alert.Indicator>
                  <CheckIcon className="size-5" />
                </Alert.Indicator>
                <Alert.Content>
                  <Alert.Title>提取完成</Alert.Title>
                  <Alert.Description>{response.message}</Alert.Description>
                </Alert.Content>
              </Alert>
            )}

            {state === "warning" && work && (
              <Alert status="warning">
                <Alert.Indicator>
                  <XIcon className="size-5" />
                </Alert.Indicator>
                <Alert.Content>
                  <Alert.Title>作品信息已提取，媒体下载失败</Alert.Title>
                  <Alert.Description>{work.downloadError}</Alert.Description>
                </Alert.Content>
              </Alert>
            )}

            {(state === "success" || state === "warning") && work && (
              <Card className="glass-panel min-w-0 border border-white/80 dark:border-white/10">
                <Card.Header className="min-w-0 items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <Card.Title className="break-words text-xl [overflow-wrap:anywhere]">
                      {work.title}
                    </Card.Title>
                    <div className="mt-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
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
                        <span className="break-words [overflow-wrap:anywhere]">
                          {work.authorName}
                        </span>
                      )}
                      <span aria-hidden="true">·</span>
                      <span>发布 {work.publishedAt}</span>
                      <span aria-hidden="true">·</span>
                      <span>更新 {work.updatedAt}</span>
                    </div>
                  </div>
                  <Chip
                    className="max-w-32 shrink-0"
                    color="accent"
                    size="sm"
                    variant="soft"
                  >
                    <Chip.Label className="truncate">{work.type}</Chip.Label>
                  </Chip>
                </Card.Header>
                <Card.Content className="grid min-w-0 gap-5">
                  <p className="whitespace-pre-wrap break-words text-sm leading-7 text-zinc-600 [overflow-wrap:anywhere] dark:text-zinc-300">
                    {work.description}
                  </p>

                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                    {Object.entries(work.metrics).map(([key, value]) => (
                      <div
                        className="min-w-0 rounded-xl bg-zinc-100 p-3 dark:bg-white/5"
                        key={key}
                      >
                        <p className="truncate text-lg font-semibold">{value}</p>
                        <p className="text-xs text-zinc-500">
                          {metricLabels[key as keyof typeof metricLabels]}
                        </p>
                      </div>
                    ))}
                  </div>

                  {work.tags.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                      {work.tags.map((tag, index) => (
                        <Chip
                          className="max-w-full"
                          key={tag + "-" + index}
                          size="sm"
                          variant="tertiary"
                        >
                          <Chip.Label className="truncate">#{tag}</Chip.Label>
                        </Chip>
                      ))}
                    </div>
                  )}

                  {work.media.length > 0 && (
                    <div className="grid gap-3">
                      <div className="flex items-center justify-between gap-3">
                        <h2 className="text-sm font-semibold">
                          媒体预览
                        </h2>
                        <span className="text-xs text-zinc-500">
                          共 {work.media.length} 项
                        </span>
                      </div>
                      <div className="grid gap-3 sm:grid-cols-2">
                        {work.media.slice(0, 4).map((item, index) => (
                          <MediaPreview
                            item={item}
                            key={item.url + "-" + index}
                            title={work.title}
                          />
                        ))}
                      </div>
                      <div className="media-scroll grid max-h-56 gap-2 overflow-y-auto pr-1">
                        {work.media.map((item, index) => (
                          <Link
                            className="flex min-w-0 items-center justify-between rounded-xl border border-zinc-200 px-3 py-2 text-sm dark:border-white/10"
                            href={item.url}
                            key={item.url + "-download-" + index}
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
                {work.workUrl && (
                  <Card.Footer className="flex min-w-0 flex-wrap items-center justify-between gap-3">
                    <span className="max-w-full truncate text-xs text-zinc-500">
                      作品 ID：{work.id}
                    </span>
                    <Link
                      href={work.workUrl}
                      rel="noreferrer"
                      target="_blank"
                    >
                      查看原作品
                      <ArrowUpRightIcon className="size-4" />
                    </Link>
                  </Card.Footer>
                )}
              </Card>
            )}
          </section>
        </div>
      </main>
    </div>
  );
}
