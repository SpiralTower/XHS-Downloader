import {
  Alert,
  Button,
  Description,
  Disclosure,
  FieldError,
  Form,
  Input,
  InputGroup,
  Label,
  Spinner,
  TextField,
} from "@heroui/react";
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useLoaderData } from "react-router";

import { ApiError, extractDetail } from "../api";
import ExtractResult from "../features/extraction/ExtractResult";
import PopularWorksSection from "../features/extraction/PopularWorksSection";
import {
  CheckIcon,
  SearchIcon,
  ShieldIcon,
  XIcon,
} from "../icons";
import type {
  ExtractionResponse,
  PublicHomeData,
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
  const { access, popular } = useLoaderData() as PublicHomeData;
  const [values, setValues] = useState(initialValues);
  const [state, setState] = useState<RequestState>("idle");
  const [response, setResponse] = useState<ExtractionResponse | null>(null);
  const [error, setError] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const controllerRef = useRef<AbortController | null>(null);
  const resultRef = useRef<HTMLDivElement>(null);

  const hasResult =
    state === "loading" ||
    state === "error" ||
    state === "success" ||
    state === "warning";

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
    <div
      className={[
        "mx-auto flex w-full max-w-3xl flex-col transition-[min-height,padding] duration-500 ease-out",
        hasResult
          ? "min-h-0 justify-start pt-2 sm:pt-4"
          : "min-h-[calc(100vh-9rem)] justify-center py-8 pb-28 sm:min-h-[calc(100vh-8rem)] sm:pb-36 -translate-y-6 sm:-translate-y-10",
      ].join(" ")}
    >
      <section
        className={[
          "flex flex-col items-center text-center transition-all duration-500 ease-out",
          hasResult ? "mb-5 gap-1.5" : "mb-7 gap-3 sm:mb-9 sm:gap-3.5",
        ].join(" ")}
      >
        <h1
          className={[
            "font-semibold tracking-[-0.04em] text-foreground outline-none transition-all duration-500 ease-out",
            hasResult
              ? "text-xl sm:text-2xl"
              : "text-3xl sm:text-4xl lg:text-5xl",
          ].join(" ")}
          id="main-heading"
          tabIndex={-1}
        >
          作品解析
        </h1>
        {!hasResult && (
          <p className="max-w-md text-sm text-muted sm:text-base">
            {access.public ? "粘贴链接，立即获取内容与媒体" : "管理员解析"}
          </p>
        )}
      </section>

      <Form
        aria-label="作品解析"
        className="w-full"
        onSubmit={submit}
        validationBehavior="native"
        validationErrors={fieldErrors}
      >
        <TextField
          className="w-full"
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
          <Label className="sr-only">作品链接</Label>
          <InputGroup
            className={[
              "search-engine-bar min-h-12 w-full rounded-full border border-border bg-surface/90 shadow-sm backdrop-blur-xl transition-[border-color,box-shadow] duration-300 sm:min-h-13",
              "focus-within:border-accent/40 focus-within:shadow-md",
            ].join(" ")}
            fullWidth
          >
            <InputGroup.Input
              autoCapitalize="none"
              autoComplete="url"
              className="min-w-0 flex-1 bg-transparent ps-5 text-base sm:text-[15px]"
              inputMode="url"
              placeholder="粘贴小红书 / RedNote / xhslink 链接"
              spellCheck={false}
            />
            <InputGroup.Suffix className="flex items-center gap-1 pe-1.5 sm:gap-1.5 sm:pe-2">
              {hasResult && (
                <Button
                  className="hidden sm:inline-flex"
                  onPress={reset}
                  size="sm"
                  type="button"
                  variant="ghost"
                >
                  重置
                </Button>
              )}
              <Button
                aria-label={state === "loading" ? "解析中" : "搜索"}
                className="search-submit-btn"
                isDisabled={state === "loading"}
                size="sm"
                type="submit"
                variant="primary"
              >
                {state === "loading" ? (
                  <Spinner color="current" size="sm" />
                ) : (
                  <SearchIcon className="size-4" />
                )}
                <span className="search-submit-label">
                  {state === "loading" ? "解析中" : "搜索"}
                </span>
              </Button>
            </InputGroup.Suffix>
          </InputGroup>
          <FieldError className="mt-2 text-center" />
        </TextField>

        <div className="mt-2.5 flex justify-start px-1 sm:px-1.5">
          <Disclosure className="w-full max-w-sm">
            <Disclosure.Heading>
              <Button
                className="h-8 gap-1.5 px-2.5 text-xs text-muted"
                slot="trigger"
                size="sm"
                type="button"
                variant="ghost"
              >
                <ShieldIcon className="size-3.5" />
                高级选项
                <Disclosure.Indicator className="size-3.5" />
              </Button>
            </Disclosure.Heading>
            <Disclosure.Content>
              <Disclosure.Body className="mt-2 grid gap-3 rounded-2xl border border-border bg-surface-secondary/90 p-3.5 text-start shadow-sm">
                <TextField
                  fullWidth
                  name="cookie"
                  onChange={(cookie) => patchValues({ cookie })}
                  value={values.cookie}
                >
                  <Label>Cookie</Label>
                  <Input
                    autoComplete="off"
                    placeholder="留空使用服务端默认"
                    spellCheck={false}
                    type="password"
                  />
                </TextField>
                <TextField
                  fullWidth
                  name="proxy"
                  onChange={(proxy) => patchValues({ proxy })}
                  value={values.proxy}
                >
                  <Label>代理</Label>
                  <Input
                    autoCapitalize="none"
                    autoComplete="off"
                    placeholder="留空使用服务端默认"
                    spellCheck={false}
                    type="password"
                  />
                  <FieldError />
                </TextField>
                <Description className="text-xs">
                  仅覆盖本次请求，不会写入历史。
                </Description>
              </Disclosure.Body>
            </Disclosure.Content>
          </Disclosure>
        </div>
      </Form>

      {state === "idle" && popular?.enabled && (
        <PopularWorksSection popular={popular} />
      )}

      <div
        aria-live={state === "error" ? "assertive" : "polite"}
        className={[
          "outline-none",
          hasResult ? "mt-8 animate-fade-in-up" : "mt-0",
        ].join(" ")}
        ref={resultRef}
        tabIndex={-1}
      >
        {state === "loading" && (
          <div className="flex flex-col items-center gap-3 py-12 text-muted">
            <Spinner color="accent" size="lg" />
            <p className="text-sm">正在获取作品…</p>
          </div>
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
  );
}
