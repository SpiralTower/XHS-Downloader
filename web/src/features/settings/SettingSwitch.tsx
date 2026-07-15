import { Description, Switch } from "@heroui/react";

export default function SettingSwitch({
  description,
  isDisabled = false,
  isSelected,
  label,
  onChange,
}: {
  description: string;
  isDisabled?: boolean;
  isSelected: boolean;
  label: string;
  onChange: (selected: boolean) => void;
}) {
  return (
    <Switch
      className="rounded-xl border border-border bg-surface-secondary p-3.5"
      isDisabled={isDisabled}
      isSelected={isSelected}
      onChange={onChange}
    >
      <Switch.Content className="flex items-center gap-3">
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        <span className="font-medium">{label}</span>
      </Switch.Content>
      <Description className="mt-1 ps-11 text-sm">{description}</Description>
    </Switch>
  );
}
