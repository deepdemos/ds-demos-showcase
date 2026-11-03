import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test } from 'vitest';
import { controls } from '@/components/ui/registry';
import { DevRoomPage, PAGE_SIZE } from './DevRoomPage';

afterEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove('ring-ui-theme-dark');
});

/**
 * 这些断言全部从 `controls` 推导，不写死具体的控件条数。
 * 写死的话，每加一个控件就要改一次测试——那种测试守着的是「控件数量没变」，
 * 而不是「分页逻辑对」，加控件时挡路、真出问题时又抓不到。
 */
const totalPages = Math.ceil(controls.length / PAGE_SIZE);
const countOnPage = (page: number) =>
  Math.max(0, Math.min(PAGE_SIZE, controls.length - (page - 1) * PAGE_SIZE));
const pagination = () => within(screen.getByRole('navigation', { name: '控件分页' }));

test('支持分类筛选、分页与主题切换', () => {
  render(
    <MemoryRouter initialEntries={['/dev-room']}>
      <DevRoomPage />
    </MemoryRouter>,
  );

  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(countOnPage(1));
  expect(screen.getByText(`第 1 / ${totalPages} 页`)).toBeInTheDocument();

  fireEvent.click(pagination().getByRole('button', { name: '下一页' }));
  expect(screen.getByText(`第 2 / ${totalPages} 页`)).toBeInTheDocument();
  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(countOnPage(2));

  // 跳到末页，确认最后一页只渲染剩下的那些
  fireEvent.click(pagination().getByRole('button', { name: '下一页' }));
  fireEvent.click(pagination().getByRole('button', { name: '下一页' }));
  expect(screen.getByText(`第 ${totalPages} / ${totalPages} 页`)).toBeInTheDocument();
  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(countOnPage(totalPages));

  // 输入类控件的注册项数从 registry 推导，不写死数字：
  // 每加一种输入样式这里都要跟着改，写死等于给未来的自己埋一次假红。
  fireEvent.click(screen.getByRole('button', { name: '输入' }));
  const inputCount = controls.filter((item) => item.category === 'input').length;
  expect(screen.getByText(`共 ${inputCount} 个控件`)).toBeInTheDocument();
  expect(document.querySelectorAll('[data-control-id]')).toHaveLength(inputCount);

  fireEvent.click(screen.getByRole('button', { name: '切换到深色模式' }));
  expect(document.documentElement).toHaveClass('ring-ui-theme-dark');

  fireEvent.click(screen.getByRole('button', { name: '切换到浅色模式' }));
  expect(document.documentElement).not.toHaveClass('ring-ui-theme-dark');
});
