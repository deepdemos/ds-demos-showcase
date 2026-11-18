import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, test } from 'vitest';
import { CommunityGuidelinesPage } from './CommunityGuidelinesPage';

describe('CommunityGuidelinesPage', () => {
  test('渲染规范与申诉说明', () => {
    render(
      <MemoryRouter>
        <CommunityGuidelinesPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: '社区规范' })).toBeInTheDocument();
    expect(screen.getByText('处置与申诉')).toBeInTheDocument();
    expect(screen.getByText(/提交一次申诉/)).toBeInTheDocument();
  });
});
