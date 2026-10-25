import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { expect, test } from 'vitest';
import { controls } from '@/components/ui/registry';
import { DevRoomPage } from './DevRoomPage';

test('renders every registered control with its stable id', () => {
  render(
    <MemoryRouter>
      <DevRoomPage />
    </MemoryRouter>,
  );

  for (const control of controls) {
    expect(screen.getByRole('heading', { level: 2, name: control.name })).toBeInTheDocument();

    const card = document.querySelector(`[data-control-id="${control.id}"]`);
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText(`#${control.id}`)).toBeInTheDocument();
  }
});
