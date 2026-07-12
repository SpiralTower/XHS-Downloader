import type {
  ExtractParams,
  ExtractResponse,
  MediaItem,
  WorkData,
  WorkViewModel,
} from "./types";

export class ApiError extends Error {
  readonly status: number | null;

  constructor(message: string, status: number | null = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readBody(response: Response): Promise<unknown> {
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    return response.json();
  }
  const text = await response.text();
  return text || null;
}

function responseMessage(body: unknown, fallback: string): string {
  if (isRecord(body)) {
    const value = body.message ?? body.error ?? body.detail;
    if (typeof value === "string" && value.trim()) return value;
  }
  if (typeof body === "string" && body.trim()) return body;
  return fallback;
}

export async function checkHealth(signal?: AbortSignal): Promise<number> {
  const startedAt = performance.now();
  const response = await fetch("/healthz", {
    method: "GET",
    cache: "no-store",
    headers: { Accept: "application/json" },
    signal,
  });
  const body = await readBody(response);
  if (!response.ok) {
    throw new ApiError(responseMessage(body, "API 健康检查失败"), response.status);
  }
  if (!isRecord(body) || body.status !== "ok") {
    throw new ApiError("API 返回了非预期的健康状态", response.status);
  }
  return Math.max(1, Math.round(performance.now() - startedAt));
}

export async function extractDetail(
  params: ExtractParams,
  signal?: AbortSignal,
): Promise<ExtractResponse> {
  const response = await fetch("/xhs/detail", {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(params),
    signal,
  });
  const body = await readBody(response);

  if (!response.ok) {
    throw new ApiError(
      responseMessage(body, `请求失败（HTTP ${response.status}）`),
      response.status,
    );
  }
  if (!isRecord(body) || typeof body.message !== "string") {
    throw new ApiError("API 返回格式无效", response.status);
  }

  return {
    message: body.message,
    params: isRecord(body.params)
      ? (body.params as unknown as ExtractParams)
      : params,
    data: isRecord(body.data) ? (body.data as WorkData) : null,
  };
}

export function parseIndexes(value: string): number[] | null {
  const source = value.trim();
  if (!source) return null;

  const result = new Set<number>();
  const tokens = source.replaceAll("，", ",").split(/[\s,]+/).filter(Boolean);

  for (const token of tokens) {
    const range = token.match(/^(\d+)-(\d+)$/);
    if (range) {
      const start = Number(range[1]);
      const end = Number(range[2]);
      if (
        !Number.isSafeInteger(start) ||
        !Number.isSafeInteger(end) ||
        start < 1 ||
        end < start ||
        end - start > 99
      ) {
        throw new Error(`图片范围“${token}”无效`);
      }
      for (let index = start; index <= end; index += 1) result.add(index);
      continue;
    }

    if (!/^\d+$/.test(token)) throw new Error(`图片序号“${token}”无效`);
    const index = Number(token);
    if (!Number.isSafeInteger(index) || index < 1) {
      throw new Error("图片序号必须是大于 0 的整数");
    }
    result.add(index);
  }

  return [...result].sort((a, b) => a - b);
}

export function getSkipMessage(response: ExtractResponse): string | null {
  if (!response.params.skip || !response.data) return null;
  const message = text(response.data.message);
  const keys = Object.keys(response.data);
  return message && keys.every((key) => key === "message") ? message : null;
}

export function getDownloadError(data: WorkData): string {
  return text(data.下载错误);
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

export function toWorkView(data: WorkData): WorkViewModel {
  const type = text(data.作品类型, "未知类型");
  const downloadUrls = urlList(data.下载地址);
  const liveUrls = Array.isArray(data.动图地址)
    ? data.动图地址.map(httpUrl).filter(Boolean)
    : [];

  const media: MediaItem[] = [
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
    downloadError: getDownloadError(data),
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
