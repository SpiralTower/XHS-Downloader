import {
  Button,
  Card,
  FieldError,
  Form,
  Input,
  Label,
  Spinner,
  TextField,
  toast,
} from "@heroui/react";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { ApiError, loginAdmin } from "../api";
import { safeNextPath } from "../app/safeNextPath";
import AppHeader from "../components/AppHeader";

export default function AdminLoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    document.title = "管理端登录 · XHS Downloader";
  }, []);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
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
        toast.danger(
          cause.status === 401
            ? "用户名或密码不正确"
            : cause.status === 503
              ? "管理端凭据尚未配置"
              : cause.message,
        );
      } else {
        toast.danger(
          cause instanceof Error ? cause.message : "登录失败，请稍后重试",
        );
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="app-shell relative min-h-screen overflow-hidden bg-background text-foreground">
      <div className="surface-grid pointer-events-none absolute inset-0" />
      <div className="relative flex min-h-screen flex-col">
        <AppHeader />
        <main
          className="mx-auto grid w-full max-w-6xl flex-1 place-items-center px-4 py-10 sm:px-6 lg:px-8"
          id="main-content"
        >
          <Card className="glass-panel w-full max-w-md border border-border">
            <Card.Header>
              <Card.Title id="main-heading" tabIndex={-1}>
                登录
              </Card.Title>
              <Card.Description>访问控制台</Card.Description>
            </Card.Header>
            <Form
              aria-labelledby="main-heading"
              className="flex w-full flex-col"
              onSubmit={submit}
              validationBehavior="native"
              validationErrors={fieldErrors}
            >
              <Card.Content>
                <div className="flex flex-col gap-4">
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
                      placeholder="admin"
                      spellCheck={false}
                      variant="secondary"
                    />
                    <FieldError />
                  </TextField>

                  <TextField
                    fullWidth
                    isRequired
                    name="password"
                    onChange={setPassword}
                    type="password"
                    value={password}
                  >
                    <Label>密码</Label>
                    <Input
                      autoComplete="current-password"
                      placeholder="••••••••"
                      type="password"
                      variant="secondary"
                    />
                    <FieldError />
                  </TextField>
                </div>
              </Card.Content>
              <Card.Footer className="flex flex-col gap-3 pt-4">
                <Button
                  fullWidth
                  isDisabled={isSubmitting}
                  type="submit"
                  variant="primary"
                >
                  {isSubmitting && <Spinner color="current" size="sm" />}
                  {isSubmitting ? "登录中…" : "登录"}
                </Button>
                <Button
                  fullWidth
                  onPress={() => navigate("/")}
                  type="button"
                  variant="ghost"
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
