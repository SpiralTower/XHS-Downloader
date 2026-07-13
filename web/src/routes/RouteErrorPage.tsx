import { Alert, Button, Card } from "@heroui/react";
import { isRouteErrorResponse, useNavigate, useRouteError } from "react-router";

import { ApiError } from "../api";
import { XIcon } from "../icons";

export default function RouteErrorPage() {
  const error = useRouteError();
  const navigate = useNavigate();

  let status = 500;
  let message = "页面加载失败，请稍后重试。";
  if (isRouteErrorResponse(error)) {
    status = error.status;
    message =
      typeof error.data === "string"
        ? error.data
        : error.statusText || message;
  } else if (error instanceof ApiError) {
    status = error.status ?? 500;
    message = error.message;
  } else if (error instanceof Error) {
    message = error.message;
  }

  return (
    <main className="app-shell grid min-h-screen place-items-center bg-background p-4 text-foreground">
      <Card className="glass-panel w-full max-w-xl border border-border">
        <Card.Header>
          <Card.Title id="main-heading" tabIndex={-1}>
            无法打开此页面
          </Card.Title>
          <Card.Description>HTTP {status}</Card.Description>
        </Card.Header>
        <Card.Content>
          <Alert status="danger">
            <Alert.Indicator>
              <XIcon className="size-5" />
            </Alert.Indicator>
            <Alert.Content>
              <Alert.Title>请求失败</Alert.Title>
              <Alert.Description>{message}</Alert.Description>
            </Alert.Content>
          </Alert>
        </Card.Content>
        <Card.Footer className="flex gap-3">
          <Button onPress={() => navigate(-1)} variant="secondary">
            返回
          </Button>
          <Button onPress={() => window.location.reload()} variant="primary">
            重新加载
          </Button>
        </Card.Footer>
      </Card>
    </main>
  );
}
