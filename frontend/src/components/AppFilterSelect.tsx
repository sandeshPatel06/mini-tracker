import { AppIcon } from './AppIcon';
import { CustomSelect } from './CustomSelect';
import type { SelectOption } from './CustomSelect';
import { Icon } from './Icon';

interface AppFilterSelectProps {
  apps: string[];
  value: string;
  onChange: (value: string) => void;
}

export function AppFilterSelect({ apps, value, onChange }: AppFilterSelectProps) {
  const options: SelectOption[] = [
    {
      value: 'ALL',
      label: 'All Applications',
      triggerLabel: `All Applications (${apps.length})`,
      leading: <Icon name="desktop" size={14} />,
      meta: String(apps.length),
    },
    ...apps.map((app) => ({
      value: app,
      label: app,
      leading: <AppIcon name={app} size={16} />,
    })),
  ];

  return (
    <CustomSelect
      className="app-filter"
      value={value}
      options={options}
      onChange={onChange}
      ariaLabel="Filter by application"
    />
  );
}
