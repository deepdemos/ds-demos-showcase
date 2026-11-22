import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { describe, expect, test } from 'vitest';
import { RedirectPage } from './RedirectPage';

function Probe() {
  const location = useLocation();
  return <p>{location.pathname + location.search + location.hash}</p>;
}

describe('RedirectPage', () => {
  test('跳到新地址并保留查询串', async () => {
    render(
      <MemoryRouter initialEntries={['/collections/7?state=deleted#top']}>
        <Routes>
          <Route path="/collections/:id" element={<RedirectPage to="/me/collections/:id" />} />
          <Route path="/me/collections/:id" element={<Probe />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText('/me/collections/7?state=deleted#top')).toBeInTheDocument();
  });

  test('无参数的旧地址也能跳', async () => {
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <Routes>
          <Route path="/admin" element={<RedirectPage to="/admin/inbox" />} />
          <Route path="/admin/inbox" element={<Probe />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText('/admin/inbox')).toBeInTheDocument();
  });
});
