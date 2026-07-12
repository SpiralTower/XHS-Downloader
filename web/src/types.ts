export interface ExtractParams {
  url: string;
  download: boolean;
  index: Array<number | string> | null;
  cookie: string | null;
  proxy: string | null;
  skip: boolean;
}

export interface WorkData extends Record<string, unknown> {
  message?: string;
  收藏数量?: string | number;
  评论数量?: string | number;
  分享数量?: string | number;
  点赞数量?: string | number;
  作品标签?: string | string[];
  作品ID?: string;
  作品链接?: string;
  作品标题?: string;
  作品描述?: string;
  作品类型?: string;
  发布时间?: string;
  最后更新时间?: string;
  时间戳?: number | null;
  作者昵称?: string;
  作者ID?: string;
  作者链接?: string;
  下载地址?: string[];
  动图地址?: Array<string | null>;
  下载错误?: string;
}

export interface ExtractResponse {
  message: string;
  params: ExtractParams;
  data: WorkData | null;
}

export interface FormValues {
  url: string;
  download: boolean;
  indexes: string;
  cookie: string;
  proxy: string;
  skip: boolean;
}

export type RequestState =
  | "idle"
  | "loading"
  | "success"
  | "warning"
  | "skipped"
  | "error";

export type HealthState = "checking" | "online" | "offline";

export interface HealthSnapshot {
  state: HealthState;
  latency: number | null;
  checkedAt: Date | null;
}

export interface MediaItem {
  url: string;
  kind: "image" | "video";
  label: string;
  isLive: boolean;
}

export interface WorkViewModel {
  id: string;
  title: string;
  description: string;
  type: string;
  workUrl: string;
  authorName: string;
  authorId: string;
  authorUrl: string;
  publishedAt: string;
  updatedAt: string;
  downloadError: string;
  tags: string[];
  metrics: {
    liked: string;
    collected: string;
    comments: string;
    shared: string;
  };
  media: MediaItem[];
}
