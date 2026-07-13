import type {
  AccessSnapshot,
  AdminSession,
  AdminSettings,
  AdminSettingsUpdate,
  ExtractionRequest,
  ExtractionResource,
  ExtractionResponse,
  HealthSnapshot,
  HistoryPage,
  MediaItem,
  WorkData,
  WorkHistory,
  WorkViewModel,
} from "./types";

export class ApiError extends Error {
  readonly status: number | null;
  readonly code: string;
  readonly fieldErrors: Record<string, string>;

  constructor(
    message: string,
    status: number | null = null,
    code = "",
    fieldErrors: Record<string, string> = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.fieldErrors = fieldErrors;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readBody(response: Response): Promise<unknown> {
  if (response.status === 204) return null;
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    return response.json();
  }
  const body = await response.text();
  return body || null;
}

function responseMessage(body: unknown, fallback: string): string {
  if (isRecord(body)) {
    const value = body.message ?? body.error ?? body.detail;
    if (typeof value === "string" && value.trim()) return value;
  }
  if (typeof body === "string" && body.trim()) return body;
  return fallback;
}

function responseCode(body: unknown): string {
  return isRecord(body) && typeof body.code === "string" ? body.code : "";
}

function responseFieldErrors(body: unknown): Record<string, string> {
  if (!isRecord(body) || !isRecord(body.field_errors)) return {};
  return Object.fromEntries(
    Object.entries(body.field_errors).filter(
      (entry): entry is [string, string] => typeof entry[1] === "string",
    ),
  );
}

async function requestJSON<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body != null && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, {
    ...init,
    cache: "no-store",
    credentials: "same-origin",
    headers,
  });
  const body = await readBody(response);

  if (!response.ok) {
    throw new ApiError(
      responseMessage(body, `请求失败（HTTP ${response.status}）`),
      response.status,
      responseCode(body),
      responseFieldErrors(body),
    );
  }
  return body as T;
}

export async function checkHealth(signal?: AbortSignal): Promise<number> {
  const startedAt = performance.now();
  const body = await requestJSON<unknown>("/healthz", {
    method: "GET",
    signal,
  });
  if (!isRecord(body) || body.status !== "ok") {
    throw new ApiError("API 返回了非预期的健康状态");
  }
  return Math.max(1, Math.round(performance.now() - startedAt));
}

export function getAccess(signal?: AbortSignal): Promise<AccessSnapshot> {
  return requestJSON("/api/v1/access", { method: "GET", signal });
}

export function getAdminSession(signal?: AbortSignal): Promise<AdminSession> {
  return requestJSON("/api/admin/v1/auth/session", {
    method: "GET",
    signal,
  });
}

export function loginAdmin(
  credentials: { username: string; password: string },
  signal?: AbortSignal,
): Promise<AdminSession> {
  return requestJSON("/api/admin/v1/auth/session", {
    method: "POST",
    body: JSON.stringify(credentials),
    signal,
  });
}

export function logoutAdmin(signal?: AbortSignal): Promise<void> {
  return requestJSON("/api/admin/v1/auth/session", {
    method: "DELETE",
    signal,
  });
}

export function extractDetail(
  params: ExtractionRequest,
  signal?: AbortSignal,
): Promise<ExtractionResponse> {
  return requestJSON("/api/v1/extractions", {
    method: "POST",
    body: JSON.stringify(params),
    signal,
  });
}

export function getAdminSettings(
  signal?: AbortSignal,
): Promise<AdminSettings> {
  return requestJSON("/api/admin/v1/settings", {
    method: "GET",
    signal,
  });
}

export function updateAdminSettings(
  update: AdminSettingsUpdate,
  signal?: AbortSignal,
): Promise<AdminSettings> {
  return requestJSON("/api/admin/v1/settings", {
    method: "PATCH",
    body: JSON.stringify(update),
    signal,
  });
}

export function getHistory(
  cursor: string | null,
  limit = 25,
  signal?: AbortSignal,
): Promise<HistoryPage> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return requestJSON(`/api/admin/v1/history?${params}`, {
    method: "GET",
    signal,
  });
}

export function getWorkHistory(
  workID: string | number,
  signal?: AbortSignal,
): Promise<WorkHistory> {
  return requestJSON(
    `/api/admin/v1/works/${encodeURIComponent(String(workID))}`,
    { method: "GET", signal },
  );
}

function text(value: unknown, fallback = ""): string {
  if (typeof value === "string") return value.trim() || fallback;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return fallback;
}

function displayMetric(value: unknown): string {
  const raw = text(value, "—");
  if (raw === "-1") return "—";
  const numeric = Number(raw);
  if (!Number.isFinite(numeric)) return raw;
  return new Intl.NumberFormat("zh-CN", { notation: "compact" }).format(numeric);
}

function list(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.map((item) => text(item)).filter(Boolean);
  }
  if (typeof value === "string") {
    return value.split(/\s+/).map((item) => item.trim()).filter(Boolean);
  }
  return [];
}

function httpUrl(value: unknown): string {
  const raw = text(value);
  if (!raw) return "";

  try {
    const parsed = new URL(raw.startsWith("//") ? "https:" + raw : raw);
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? parsed.href
      : "";
  } catch {
    return "";
  }
}

function urlList(value: unknown): string[] {
  return list(value).map(httpUrl).filter(Boolean);
}

function mediaKind(url: string, type: string, isLive: boolean): MediaItem["kind"] {
  if (isLive || type.includes("视频") || /\.(?:mp4|mov|m4v)(?:$|\?)/i.test(url)) {
    return "video";
  }
  return "image";
}

function resourceMedia(resource: ExtractionResource): MediaItem | null {
  const url = httpUrl(resource.remote_url);
  if (!url || resource.kind === "text") return null;
  return {
    url,
    kind: resource.kind === "image" ? "image" : "video",
    label:
      resource.kind === "image"
        ? `图片 ${resource.ordinal}`
        : `视频 ${resource.ordinal}`,
    isLive: false,
    saveStatus: resource.save_status,
  };
}

export function toWorkView(
  data: WorkData,
  resources: ExtractionResource[] = [],
): WorkViewModel {
  const type = text(data.作品类型, "未知类型");
  const downloadUrls = urlList(data.下载地址);
  const liveUrls = Array.isArray(data.动图地址)
    ? data.动图地址.map(httpUrl).filter(Boolean)
    : [];

  const fromData: MediaItem[] = [
    ...downloadUrls.map((url, index) => ({
      url,
      kind: mediaKind(url, type, false),
      label: type.includes("视频") ? "视频文件" : `图片 ${index + 1}`,
      isLive: false,
    } satisfies MediaItem)),
    ...liveUrls.map((url, index) => ({
      url,
      kind: mediaKind(url, type, true),
      label: `动态内容 ${index + 1}`,
      isLive: true,
    } satisfies MediaItem)),
  ];

  const media = [...fromData];
  const mediaByURL = new Map(media.map((item) => [item.url, item]));
  for (const resource of resources) {
    const item = resourceMedia(resource);
    if (!item) continue;

    const existing = mediaByURL.get(item.url);
    if (existing) {
      existing.saveStatus = item.saveStatus;
      continue;
    }

    mediaByURL.set(item.url, item);
    media.push(item);
  }

  return {
    id: text(data.作品ID, "未知 ID"),
    title: text(data.作品标题, "未命名作品"),
    description: text(data.作品描述, "该作品没有提供文字描述。"),
    type,
    workUrl: httpUrl(data.作品链接),
    authorName: text(data.作者昵称, "未知作者"),
    authorId: text(data.作者ID, "—"),
    authorUrl: httpUrl(data.作者链接),
    publishedAt: text(data.发布时间, "未知").replace("_", " "),
    updatedAt: text(data.最后更新时间, "未知").replace("_", " "),
    downloadError: text(data.下载错误),
    tags: list(data.作品标签).map((tag) => tag.replace(/^#/, "")),
    metrics: {
      liked: displayMetric(data.点赞数量),
      collected: displayMetric(data.收藏数量),
      comments: displayMetric(data.评论数量),
      shared: displayMetric(data.分享数量),
    },
    media,
  };
}

export function formatDateTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function emptyHealthSnapshot(): HealthSnapshot {
  return { state: "checking", latency: null, checkedAt: null };
}
