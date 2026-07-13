import {
  Alert,
  Button,
  Card,
  FieldError,
  Form,
  Input,
  Label,
  Spinner,
  TextField,
} from "@heroui/react";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { ApiError, loginAdmin } from "../api";
import { safeNextPath } from "../app/safeNextPath";
import AppHeader from "../components/AppHeader";
import { ShieldIcon, XIcon } from "../icons";

export default function AdminLoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    document.title = "管理端登录 · XHS Downloader";
  }, []);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setFieldErrors({});
    setIsSubmitting(true);

    try {
      await loginAdmin({ username: username.trim(), password });
      navigate(
        safeNextPath(searchParams.get("next"), "/admin/settings"),
        { replace: true },
      );
    } catch (cause) {
      if (cause instanceof ApiError) {
        setFieldErrors(cause.fieldErrors);
        setError(
          cause.status === 401
            ? "用户名或密码不正确"
            : cause.status === 503
              ? "管理端凭据尚未配置，请先配置服务端环境变量"
              : cause.message,
        );
      } else {
        setError(cause instanceof Error ? cause.message : "登录失败，请稍后重试");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="app-shell relative min-h-screen overflow-hidden bg-background text-foreground">
      <div className="surface-grid pointer-events-none absolute inset-0" />
      <div className="relative">
        <AppHeader />
        <main
          className="mx-auto grid min-h-[calc(100vh-5rem)] w-full max-w-7xl place-items-center px-4 py-10 sm:px-6 lg:px-8"
          id="main-content"
        >
          <Card className="glass-panel w-full max-w-md border border-border">
            <Card.Header>
              <span className="grid size-11 place-items-center rounded-2xl bg-accent-soft text-accent-soft-foreground">
                <ShieldIcon className="size-5" />
              </span>
              <div>
                <Card.Title
                  className="outline-none"
                  id="main-heading"
                  tabIndex={-1}
                >
                  登录管理端
                </Card.Title>
                <Card.Description>
                  管理默认连接、保存策略和解析历史。
                </Card.Description>
              </div>
            </Card.Header>

            <Form
              aria-labelledby="main-heading"
              onSubmit={submit}
              validationBehavior="native"
              validationErrors={fieldErrors}
            >
              <Card.Content className="grid gap-4">
                {error && (
                  <Alert status="danger">
                    <Alert.Indicator>
                      <XIcon className="size-5" />
                    </Alert.Indicator>
                    <Alert.Content>
                      <Alert.Title>登录失败</Alert.Title>
                      <Alert.Description>{error}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                )}

                <TextField
                  fullWidth
                  isRequired
                  name="username"
                  onChange={setUsername}
                  value={username}
                >
                  <Label>用户名</Label>
                  <Input
                    autoCapitalize="none"
                    autoComplete="username"
                    spellCheck={false}
                  />
                  <FieldError />
                </TextField>

                <TextField
                  fullWidth
                  isRequired
                  name="password"
                  onChange={setPassword}
                  value={password}
                >
                  <Label>密码</Label>
                  <Input autoComplete="current-password" type="password" />
                  <FieldError />
                </TextField>
              </Card.Content>

              <Card.Footer className="flex gap-3">
                <Button
                  fullWidth
                  isDisabled={isSubmitting}
                  type="submit"
                  variant="primary"
                >
                  {isSubmitting && <Spinner color="current" size="sm" />}
                  {isSubmitting ? "正在登录…" : "登录"}
                </Button>
                <Button
                  onPress={() => navigate("/")}
                  type="button"
                  variant="secondary"
                >
                  返回首页
                </Button>
              </Card.Footer>
            </Form>
          </Card>
        </main>
      </div>
    </div>
  );
}
