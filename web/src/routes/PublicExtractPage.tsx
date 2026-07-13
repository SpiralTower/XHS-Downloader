import {
  Alert,
  Button,
  Card,
  Chip,
  Description,
  Disclosure,
  FieldError,
  Form,
  Input,
  Label,
  Spinner,
  TextField,
} from "@heroui/react";
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useLoaderData } from "react-router";

import { ApiError, extractDetail } from "../api";
import ExtractResult from "../features/extraction/ExtractResult";
import {
  CheckIcon,
  SearchIcon,
  ShieldIcon,
  SparklesIcon,
  XIcon,
} from "../icons";
import type {
  AccessSnapshot,
  ExtractionResponse,
  PublicExtractFormValues,
  RequestState,
} from "../types";

const initialValues: PublicExtractFormValues = {
  url: "",
  cookie: "",
  proxy: "",
};

const supportedLinkPattern =
  /(?:https?:\/\/)?(?:www\.)?(?:(?:xiaohongshu|rednote)\.com\/(?:explore\/\S+|discovery\/item\/\S+|user\/profile\/[a-z0-9]+\/\S+)|xhslink\.com\/\S+)/i;

function validateWorkUrl(value: string): string | null {
  const candidate = value.match(supportedLinkPattern)?.[0];
  if (!candidate) {
    return "请输入有效的小红书、RedNote 或 xhslink 作品链接";
  }
  try {
    const parsed = new URL(
      /^https?:\/\//i.test(candidate) ? candidate : `https://${candidate}`,
    );
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "作品链接必须使用 http 或 https";
    }
  } catch {
    return "请输入完整、有效的作品链接";
  }
  return null;
}

export default function PublicExtractPage() {
  const access = useLoaderData() as AccessSnapshot;
  const [values, setValues] = useState(initialValues);
  const [state, setState] = useState<RequestState>("idle");
  const [response, setResponse] = useState<ExtractionResponse | null>(null);
  const [error, setError] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const controllerRef = useRef<AbortController | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    document.title = "作品解析 · XHS Downloader";
    return () => controllerRef.current?.abort();
  }, []);

  useEffect(() => {
    if (state === "error" || state === "success" || state === "warning") {
      window.requestAnimationFrame(() => resultRef.current?.focus());
    }
  }, [state]);

  const patchValues = (patch: Partial<PublicExtractFormValues>) => {
    setValues((current) => ({ ...current, ...patch }));
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setFieldErrors({});

    const invalidURL = validateWorkUrl(values.url);
    if (invalidURL) {
      setFieldErrors({ url: invalidURL });
      return;
    }

    const connection = {
      ...(values.cookie.trim() ? { cookie: values.cookie.trim() } : {}),
      ...(values.proxy.trim() ? { proxy: values.proxy.trim() } : {}),
    };

    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setState("loading");
    setResponse(null);

    try {
      const result = await extractDetail(
        {
          url: values.url.trim(),
          ...(Object.keys(connection).length > 0 ? { connection } : {}),
        },
        controller.signal,
      );
      setResponse(result);
      setState(
        result.version.resources.some(
          (resource) => resource.save_status === "failed",
        )
          ? "warning"
          : "success",
      );
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === "AbortError") return;
      if (cause instanceof ApiError) setFieldErrors(cause.fieldErrors);
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
    setFieldErrors({});
    setState("idle");
  };

  return (
    <div className="grid gap-8">
      <section className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div className="max-w-3xl">
          <Chip color="accent" size="sm" variant="soft">
            <Chip.Label>
              {access.public ? "公共解析已开放" : "管理员专用解析"}
            </Chip.Label>
          </Chip>
          <h1
            className="mt-4 text-balance text-3xl font-bold tracking-[-0.04em] outline-none sm:text-5xl"
            id="main-heading"
            tabIndex={-1}
          >
            提交作品链接，获取结构化内容与媒体。
          </h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-muted sm:text-base">
            留空高级连接项时使用服务端默认配置；填写内容仅覆盖本次请求。
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted">
          <ShieldIcon className="size-4 text-success" />
          Cookie 与代理不会出现在响应或历史记录中
        </div>
      </section>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,0.82fr)_minmax(0,1.18fr)]">
        <Card className="glass-panel border border-border">
          <Card.Header>
            <div>
              <Card.Title>解析作品</Card.Title>
              <Card.Description>
                只需作品链接；连接参数是可选的本次覆盖值。
              </Card.Description>
            </div>
          </Card.Header>
          <Form
            aria-label="作品解析表单"
            onSubmit={submit}
            validationBehavior="native"
            validationErrors={fieldErrors}
          >
            <Card.Content className="grid gap-5">
              <TextField
                fullWidth
                isRequired
                name="url"
                onChange={(url) => {
                  patchValues({ url });
                  setFieldErrors((current) => ({ ...current, url: "" }));
                }}
                validate={validateWorkUrl}
                value={values.url}
              >
                <Label>作品链接</Label>
                <Input
                  autoCapitalize="none"
                  autoComplete="url"
                  inputMode="url"
                  placeholder="粘贴小红书、RedNote 或 xhslink 分享链接"
                  spellCheck={false}
                />
                <Description>支持完整链接和包含链接的分享文案。</Description>
                <FieldError />
              </TextField>

              <Disclosure>
                <Disclosure.Heading>
                  <Button slot="trigger" type="button" variant="secondary">
                    <ShieldIcon className="size-4" />
                    高级连接选项
                    <Disclosure.Indicator />
                  </Button>
                </Disclosure.Heading>
                <Disclosure.Content>
                  <Disclosure.Body className="mt-3 grid gap-4 rounded-2xl border border-border bg-surface-secondary p-4">
                    <TextField
                      fullWidth
                      name="cookie"
                      onChange={(cookie) => patchValues({ cookie })}
                      value={values.cookie}
                    >
                      <Label>本次 Cookie</Label>
                      <Input
                        autoComplete="off"
                        placeholder="留空则继承服务端默认 Cookie"
                        spellCheck={false}
                        type="password"
                      />
                      <Description>
                        填写时仅覆盖本次解析，不会保存到浏览器或解析历史。
                      </Description>
                    </TextField>
                    <TextField
                      fullWidth
                      name="proxy"
                      onChange={(proxy) => patchValues({ proxy })}
                      value={values.proxy}
                    >
                      <Label>本次代理地址</Label>
                      <Input
                        autoCapitalize="none"
                        autoComplete="off"
                        placeholder="留空则继承服务端默认代理"
                        spellCheck={false}
                        type="password"
                      />
                      <Description>
                        使用 http 或 https 代理；带凭据的地址将按秘密处理。
                      </Description>
                      <FieldError />
                    </TextField>
                  </Disclosure.Body>
                </Disclosure.Content>
              </Disclosure>
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
                {state === "loading" ? "正在解析…" : "开始解析"}
              </Button>
              <Button onPress={reset} type="button" variant="secondary">
                重置
              </Button>
            </Card.Footer>
          </Form>
        </Card>

        <div
          aria-live={state === "error" ? "assertive" : "polite"}
          className="grid gap-4 outline-none"
          ref={resultRef}
          tabIndex={-1}
        >
          {state === "idle" && (
            <Card className="glass-panel min-h-72 border border-dashed border-border" variant="transparent">
              <Card.Content className="grid min-h-72 place-items-center text-center">
                <div className="max-w-sm">
                  <span className="mx-auto grid size-14 place-items-center rounded-2xl bg-accent-soft text-accent-soft-foreground">
                    <SparklesIcon className="size-7" />
                  </span>
                  <h2 className="mt-4 font-semibold">等待一次解析任务</h2>
                  <p className="mt-2 text-sm leading-6 text-muted">
                    结果会在这里展示，包括作品信息、版本来源和媒体资源。
                  </p>
                </div>
              </Card.Content>
            </Card>
          )}

          {state === "loading" && (
            <Card className="glass-panel min-h-72 border border-border">
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
                <Alert.Title>解析失败</Alert.Title>
                <Alert.Description>{error}</Alert.Description>
              </Alert.Content>
            </Alert>
          )}

          {(state === "success" || state === "warning") && response && (
            <ExtractResult response={response} />
          )}

          {state === "success" && (
            <p className="sr-only">
              <CheckIcon className="size-4" />
              作品解析完成
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
