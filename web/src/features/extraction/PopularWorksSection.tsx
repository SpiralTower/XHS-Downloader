import { Card, Chip, Link, Tabs } from "@heroui/react";

import type { PopularWork, PopularWorks } from "../../types";

const countFormatter = new Intl.NumberFormat("zh-CN");

function PopularList({
  emptyMessage,
  items,
}: {
  emptyMessage: string;
  items: PopularWork[];
}) {
  if (items.length === 0) {
    return (
      <div className="grid min-h-44 place-items-center rounded-xl bg-surface-secondary px-5 text-center text-sm text-muted">
        {emptyMessage}
      </div>
    );
  }

  return (
    <ol className="grid w-full gap-1">
      {items.map((item, index) => (
        <li key={item.platform_id}>
          <Link
            aria-label={`打开第 ${index + 1} 名作品：${item.title || item.platform_id}（新标签页）`}
            className="flex w-full min-w-0 items-center gap-3 rounded-xl px-2 py-2.5 text-foreground no-underline hover:bg-surface-secondary"
            href={item.work_url}
            rel="noreferrer"
            target="_blank"
          >
            <span
              aria-hidden
              className="grid size-7 shrink-0 place-items-center rounded-lg bg-accent-soft text-xs font-semibold text-accent-soft-foreground"
            >
              {index + 1}
            </span>
            <div className="min-w-0 flex-1">
              <p
                className="truncate text-sm font-medium"
                title={item.title || "未保存标题"}
              >
                {item.title || "未保存标题"}
              </p>
              <p className="mt-0.5 truncate font-mono text-xs text-muted">
                {item.platform_id}
              </p>
            </div>
            <Chip color="accent" size="sm" variant="soft">
              <Chip.Label>
                {countFormatter.format(item.parse_count)} 次
              </Chip.Label>
            </Chip>
            <Link.Icon className="size-3.5 shrink-0 text-muted" />
          </Link>
        </li>
      ))}
    </ol>
  );
}

export default function PopularWorksSection({
  popular,
}: {
  popular: PopularWorks;
}) {
  return (
    <section
      aria-label="热门解析榜单"
      className="mt-9 grid w-full gap-4 text-start md:grid-cols-2"
    >
      <Card className="glass-panel min-w-0 border border-border">
        <Card.Header>
          <Card.Title>历史热门</Card.Title>
        </Card.Header>
        <Card.Content>
          <PopularList
            emptyMessage="暂无历史解析排行"
            items={popular.all_time}
          />
        </Card.Content>
      </Card>

      <Card className="glass-panel min-w-0 border border-border">
        <Tabs
          className="flex w-full flex-col"
          defaultSelectedKey="30d"
          variant="secondary"
        >
          <Card.Header className="flex-row items-center justify-between gap-4">
            <Card.Title>近期热门</Card.Title>
            <Tabs.ListContainer className="w-auto max-w-full shrink-0">
              <Tabs.List
                aria-label="近期榜单时间范围"
                className="w-fit *:h-6 *:w-fit *:px-3 *:text-sm"
              >
                <Tabs.Tab id="30d">
                  近 30 天
                  <Tabs.Indicator />
                </Tabs.Tab>
                <Tabs.Tab id="7d">
                  近 7 天
                  <Tabs.Indicator />
                </Tabs.Tab>
              </Tabs.List>
            </Tabs.ListContainer>
          </Card.Header>
          <Card.Content>
            <Tabs.Panel id="30d">
              <PopularList
                emptyMessage="近 30 天暂无解析排行"
                items={popular.recent_30d}
              />
            </Tabs.Panel>
            <Tabs.Panel id="7d">
              <PopularList
                emptyMessage="近 7 天暂无解析排行"
                items={popular.recent_7d}
              />
            </Tabs.Panel>
          </Card.Content>
        </Tabs>
      </Card>
    </section>
  );
}
