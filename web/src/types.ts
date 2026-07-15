export interface AccessSnapshot {
  public: boolean;
  authenticated: boolean;
  can_extract: boolean;
}

export interface AdminSession {
  authenticated: boolean;
  username?: string;
  expires_at?: string;
}

export interface ConnectionOverrides {
  cookie?: string;
  proxy?: string;
}

export interface ExtractionRequest {
  url: string;
  connection?: ConnectionOverrides;
}

export type ConnectionSource = "default" | "override" | "disabled" | "none";
export type ExtractionSource = "fetched" | "cache";
export type HistorySource = ExtractionSource | "skipped" | "";
export type ResourceKind = "text" | "image" | "video";
export type SaveStatus = "disabled" | "pending" | "saved" | "failed";

export interface ExtractionResource {
  id: number;
  kind: ResourceKind;
  ordinal: number;
  remote_url: string;
  save_status: SaveStatus;
  save_error?: string;
  mime_type?: string;
  size_bytes?: number;
  sha256?: string;
}

export interface ExtractionVersion {
  id: number;
  number: number;
  captured_at: string;
  resources: ExtractionResource[];
}

export interface WorkReference {
  id: number;
  platform_id: string;
}

export interface ExtractionResponse {
  run_id: number;
  source: ExtractionSource;
  message: string;
  connection: {
    cookie_source: ConnectionSource;
    proxy_source: ConnectionSource;
  };
  work: WorkReference;
  version: ExtractionVersion;
  data: WorkData;
}

export interface PublicExtractFormValues {
  url: string;
  cookie: string;
  proxy: string;
}

export interface SaveSettings {
  text: boolean;
  images: boolean;
  videos: boolean;
}

export interface SecretSummary {
  configured: boolean;
  display?: string;
}

export interface AdminSettings {
  revision: number;
  public: boolean;
  show_popular: boolean;
  save: SaveSettings;
  refetch: boolean;
  default_cookie: SecretSummary;
  default_proxy: SecretSummary;
}

export type SecretAction = "keep" | "replace" | "clear";

export interface SecretUpdate {
  action: SecretAction;
  value?: string;
}

export interface AdminSettingsUpdate {
  revision: number;
  public?: boolean;
  show_popular?: boolean;
  save?: Partial<SaveSettings>;
  refetch?: boolean;
  default_cookie?: SecretUpdate;
  default_proxy?: SecretUpdate;
}

export type HistoryStatus = "running" | "succeeded" | "failed";

export interface HistoryItem {
  run_id: number;
  requested_url: string;
  status: HistoryStatus;
  source: HistorySource;
  started_at: string;
  finished_at?: string;
  work?: WorkReference;
  version?: {
    id: number;
    number: number;
  };
  error?: string;
}

export interface HistoryPage {
  items: HistoryItem[];
  next_cursor: string | null;
}

export interface WorkListItem extends WorkReference {
  parse_count: number;
  version_count: number;
  last_parsed_at?: string;
  title?: string;
  thumbnail_url?: string;
}

export interface WorkPage {
  items: WorkListItem[];
  next_cursor: string | null;
}

export interface StoredWork extends WorkReference {
  first_seen_at: string;
  last_seen_at: string;
  parse_count: number;
  last_parsed_at?: string;
}

export interface StoredVersion extends ExtractionVersion {
  data: WorkData;
}

export interface WorkHistory {
  work: StoredWork;
  versions: StoredVersion[];
}

export interface PopularWork {
  platform_id: string;
  title?: string;
  work_url: string;
  parse_count: number;
}

export interface PopularWorks {
  enabled: boolean;
  all_time: PopularWork[];
  recent_30d: PopularWork[];
  recent_7d: PopularWork[];
}

export interface PublicHomeData {
  access: AccessSnapshot;
  popular: PopularWorks | null;
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
  封面地址?: string;
  下载地址?: string[];
  动图地址?: Array<string | null>;
  下载错误?: string;
}

export type RequestState = "idle" | "loading" | "success" | "warning" | "error";

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
  saveStatus?: SaveStatus;
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
