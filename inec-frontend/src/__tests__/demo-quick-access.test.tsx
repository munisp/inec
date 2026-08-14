/**
 * R4-41 regression tests: demo-credential quick-access buttons must only
 * render when explicitly enabled (LoginPage passes import.meta.env.DEV).
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import DemoQuickAccess from '../components/DemoQuickAccess';

describe('DemoQuickAccess (R4-41)', () => {
  it('renders nothing when disabled (production build path)', () => {
    const { container } = render(<DemoQuickAccess enabled={false} onQuickLogin={() => {}} />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText(/Quick access/i)).toBeNull();
    expect(screen.queryByText('Administrator')).toBeNull();
  });

  it('renders the three demo accounts when enabled (dev builds)', () => {
    render(<DemoQuickAccess enabled={true} onQuickLogin={() => {}} />);
    expect(screen.getByText(/Quick access/i)).toBeInTheDocument();
    expect(screen.getByText('Administrator')).toBeInTheDocument();
    expect(screen.getByText('Presiding Officer')).toBeInTheDocument();
    expect(screen.getByText('Election Observer')).toBeInTheDocument();
  });

  it('passes the wired demo credentials to onQuickLogin', () => {
    const onQuickLogin = vi.fn();
    render(<DemoQuickAccess enabled={true} onQuickLogin={onQuickLogin} />);
    fireEvent.click(screen.getByText('Administrator'));
    expect(onQuickLogin).toHaveBeenCalledWith('admin', 'admin123');
  });

  it('source of LoginPage gates on import.meta.env.DEV', async () => {
    // Guard against regressions where the gate is dropped from LoginPage.
    const fs = await import('node:fs/promises');
    const src = await fs.readFile('src/pages/LoginPage.tsx', 'utf8');
    expect(src).toContain('enabled={import.meta.env.DEV}');
    expect(src).not.toContain("quickLogin('admin'");
  });
});
