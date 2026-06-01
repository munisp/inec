import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useMiddlewareStore } from '../store/middleware';

describe('Middleware Store', () => {
  beforeEach(() => {
    useMiddlewareStore.setState({
      components: {},
      status: 'loading',
      lastChecked: null,
    });
  });

  it('should start in loading state', () => {
    const state = useMiddlewareStore.getState();
    expect(state.status).toBe('loading');
    expect(Object.keys(state.components)).toHaveLength(0);
  });

  it('should fetch and set healthy status when all connected', async () => {
    const mockMiddleware = {
      redis: { connected: true, mode: 'embedded' },
      kafka: { connected: true, mode: 'embedded' },
      keycloak: { connected: true, mode: 'embedded' },
    };

    global.fetch = vi.fn().mockResolvedValue({
      json: () => Promise.resolve({ checks: { middleware: mockMiddleware } }),
    });

    await useMiddlewareStore.getState().fetchStatus();

    const state = useMiddlewareStore.getState();
    expect(state.status).toBe('healthy');
    expect(Object.keys(state.components)).toHaveLength(3);
    expect(state.components.redis.connected).toBe(true);
  });

  it('should set degraded status when some disconnected', async () => {
    const mockMiddleware = {
      redis: { connected: true, mode: 'embedded' },
      kafka: { connected: false, mode: 'embedded' },
    };

    global.fetch = vi.fn().mockResolvedValue({
      json: () => Promise.resolve({ checks: { middleware: mockMiddleware } }),
    });

    await useMiddlewareStore.getState().fetchStatus();

    const state = useMiddlewareStore.getState();
    expect(state.status).toBe('degraded');
  });

  it('should set error status on fetch failure', async () => {
    global.fetch = vi.fn().mockRejectedValue(new Error('network error'));

    await useMiddlewareStore.getState().fetchStatus();

    const state = useMiddlewareStore.getState();
    expect(state.status).toBe('error');
    expect(state.lastChecked).not.toBeNull();
  });
});
