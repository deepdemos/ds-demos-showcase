import { createContext, useContext, type ReactNode } from 'react';
import type { PageWidth } from '@/app/routes';

const WIDTH_CLASS: Record<PageWidth, string> = {
  auth: 'max-w-sm',
  form: 'max-w-2xl',
  narrow: 'max-w-3xl',
  default: 'max-w-4xl',
  wide: 'max-w-6xl',
};

/** 路由层用清单里的 width 包住页面；页面自己写错也覆盖不了。 */
const PageWidthContext = createContext<PageWidth | null>(null);

export function PageWidthProvider({ width, children }: { width: PageWidth; children: ReactNode }) {
  return <PageWidthContext.Provider value={width}>{children}</PageWidthContext.Provider>;
}

export interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}

/** 页面抬头：h1、说明、一组主/次操作。 */
export function PageHeader({ title, description, actions, eyebrow }: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-3 border-b border-gray-200 pb-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        {eyebrow ? <p className="m-0 text-xs text-gray-500">{eyebrow}</p> : null}
        <h1 className="m-0 text-2xl font-semibold text-gray-900">{title}</h1>
        {description ? <p className="m-0 mt-2 text-sm text-gray-500">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  );
}

export interface PageSectionProps {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** 页面里的一段；标题与操作同一行，移动端堆叠。 */
export function PageSection({
  title,
  description,
  actions,
  children,
  className = '',
}: PageSectionProps) {
  return (
    <section className={'mt-6 ' + className}>
      {title || actions ? (
        <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div className="min-w-0">
            {title ? <h2 className="m-0 text-lg font-semibold text-gray-900">{title}</h2> : null}
            {description ? <p className="m-0 mt-1 text-sm text-gray-500">{description}</p> : null}
          </div>
          {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  );
}

export interface PageProps {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
  width?: PageWidth;
  children: ReactNode;
}

/**
 * 页面外壳：宽度、留白、抬头。
 *
 * 页面不再自己写 max-w-* 与 px-4 py-8；五档宽度是站点级决定，
 * 视觉负责人换样式时只改这里。
 */
export function Page({
  title,
  description,
  actions,
  eyebrow,
  width = 'default',
  children,
}: PageProps) {
  // 路由层已经按 routes.ts 声明了宽度时，以清单为准；独立渲染（如单测）才看 props。
  const routeWidth = useContext(PageWidthContext);
  const effectiveWidth = routeWidth ?? width;
  return (
    <div className={'mx-auto w-full px-4 py-8 sm:px-6 lg:px-8 ' + WIDTH_CLASS[effectiveWidth]}>
      <PageHeader title={title} description={description} actions={actions} eyebrow={eyebrow} />
      {children}
    </div>
  );
}
