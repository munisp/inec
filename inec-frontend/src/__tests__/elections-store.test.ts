import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useElectionsStore } from '../store/elections';

describe('Elections Store', () => {
  beforeEach(() => {
    useElectionsStore.setState({
      elections: [],
      selectedElection: null,
      loading: false,
      error: null,
    });
  });

  it('should start with empty elections', () => {
    const state = useElectionsStore.getState();
    expect(state.elections).toHaveLength(0);
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
  });

  it('should fetch elections successfully', async () => {
    const mockElections = [
      { id: 1, title: '2027 Presidential', election_type: 'presidential', election_date: '2027-02-28', status: 'active' },
      { id: 2, title: '2027 Gubernatorial', election_type: 'gubernatorial', election_date: '2027-03-11', status: 'upcoming' },
    ];

    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockElections),
    });

    await useElectionsStore.getState().fetchElections();

    const state = useElectionsStore.getState();
    expect(state.elections).toHaveLength(2);
    expect(state.elections[0].title).toBe('2027 Presidential');
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
  });

  it('should handle fetch error', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
    });

    await useElectionsStore.getState().fetchElections();

    const state = useElectionsStore.getState();
    expect(state.error).toBe('HTTP 401');
    expect(state.loading).toBe(false);
  });

  it('should select an election', () => {
    const election = { id: 1, title: 'Test', election_type: 'presidential', election_date: '2027-01-01', status: 'active' };
    useElectionsStore.getState().selectElection(election);

    const state = useElectionsStore.getState();
    expect(state.selectedElection?.id).toBe(1);
  });

  it('should set loading state during fetch', async () => {
    let resolvePromise: (value: unknown) => void;
    const promise = new Promise((resolve) => { resolvePromise = resolve; });

    global.fetch = vi.fn().mockReturnValue(promise);

    const fetchPromise = useElectionsStore.getState().fetchElections();

    // Should be loading
    expect(useElectionsStore.getState().loading).toBe(true);

    resolvePromise!({ ok: true, json: () => Promise.resolve([]) });
    await fetchPromise;

    expect(useElectionsStore.getState().loading).toBe(false);
  });
});
