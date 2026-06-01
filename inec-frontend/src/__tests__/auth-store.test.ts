import { describe, it, expect, beforeEach } from 'vitest';
import { useAuthStore } from '../store/auth';

describe('Auth Store', () => {
  beforeEach(() => {
    useAuthStore.setState({
      token: null,
      user: null,
      isAuthenticated: false,
    });
  });

  it('should start unauthenticated', () => {
    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.token).toBeNull();
    expect(state.user).toBeNull();
  });

  it('should login successfully', () => {
    const { login } = useAuthStore.getState();
    login('test-token-123', { id: 1, username: 'admin', role: 'admin' });

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.token).toBe('test-token-123');
    expect(state.user?.username).toBe('admin');
    expect(state.user?.role).toBe('admin');
  });

  it('should logout successfully', () => {
    const { login, logout } = useAuthStore.getState();
    login('test-token', { id: 1, username: 'admin', role: 'admin' });
    logout();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.token).toBeNull();
    expect(state.user).toBeNull();
  });

  it('should reject non-admin roles for admin-only operations', () => {
    const { login } = useAuthStore.getState();
    login('observer-token', { id: 2, username: 'observer', role: 'observer' });

    const state = useAuthStore.getState();
    expect(state.user?.role).toBe('observer');
    expect(state.user?.role).not.toBe('admin');
  });

  it('should persist user data on setUser', () => {
    const { setUser } = useAuthStore.getState();
    setUser({ id: 5, username: 'officer1', role: 'presiding_officer', full_name: 'Adamu Musa' });

    const state = useAuthStore.getState();
    expect(state.user?.full_name).toBe('Adamu Musa');
    expect(state.user?.role).toBe('presiding_officer');
  });
});
