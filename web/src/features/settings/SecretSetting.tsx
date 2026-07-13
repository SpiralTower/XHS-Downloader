import {
  Alert,
  Button,
  Description,
  FieldError,
  Input,
  Label,
  TextField,
} from "@heroui/react";

import { ShieldIcon } from "../../icons";
import type { SecretAction, SecretSummary } from "../../types";

export default function SecretSetting({
  action,
  description,
  fieldName,
  isDisabled,
  label,
  onActionChange,
  onValueChange,
  summary,
  value,
}: {
  action: SecretAction;
  description: string;
  fieldName: string;
  isDisabled: boolean;
  label: string;
  onActionChange: (action: SecretAction) => void;
  onValueChange: (value: string) => void;
  summary: SecretSummary;
  value: string;
}) {
  return (
    <div className="grid gap-3 rounded-2xl border border-border bg-surface-secondary p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <p className="font-medium">{label}</p>
          <p className="mt-1 text-sm text-muted">
            {summary.configured
              ? summary.display || "已配置（值不会返回浏览器）"
              : "尚未配置"}
          </p>
        </div>
        <div
          aria-label={`${label}更新方式`}
          className="flex flex-wrap gap-1"
          role="group"
        >
          <Button
            aria-pressed={action === "keep"}
            isDisabled={isDisabled}
            onPress={() => onActionChange("keep")}
            size="sm"
            type="button"
            variant={action === "keep" ? "primary" : "secondary"}
          >
            保留
          </Button>
          <Button
            aria-pressed={action === "replace"}
            isDisabled={isDisabled}
            onPress={() => onActionChange("replace")}
            size="sm"
            type="button"
            variant={action === "replace" ? "primary" : "secondary"}
          >
            {summary.configured ? "替换" : "设置"}
          </Button>
          <Button
            aria-pressed={action === "clear"}
            isDisabled={isDisabled || !summary.configured}
            onPress={() => onActionChange("clear")}
            size="sm"
            type="button"
            variant={action === "clear" ? "danger" : "secondary"}
          >
            清除
          </Button>
        </div>
      </div>

      {action === "replace" && (
        <TextField
          fullWidth
          isRequired
          name={fieldName}
          onChange={onValueChange}
          value={value}
        >
          <Label>新的{label}</Label>
          <Input
            autoCapitalize="none"
            autoComplete="off"
            spellCheck={false}
            type="password"
          />
          <Description>{description}</Description>
          <FieldError />
        </TextField>
      )}

      {action === "clear" && (
        <Alert status="warning">
          <Alert.Indicator>
            <ShieldIcon className="size-5" />
          </Alert.Indicator>
          <Alert.Content>
            <Alert.Title>保存后将清除{label}</Alert.Title>
            <Alert.Description>
              后续请求不会再继承这项默认连接配置。
            </Alert.Description>
          </Alert.Content>
        </Alert>
      )}
    </div>
  );
}
