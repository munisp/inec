import { pickLatestActiveElection } from '../election-picker';
import type { Election } from '../api-types';

const el = (id: number, status: string, date: string): Election => ({
  id, name: `E${id}`, type: 'general', date, status,
  total_polling_units: 0, results_submitted: 0, registered_voters: 0,
});

describe('pickLatestActiveElection (election resolution, R5-107/R5-112)', () => {
  it('returns null for an empty list — never fabricates an election id', () => {
    expect(pickLatestActiveElection([])).toBeNull();
  });

  it('prefers the latest ACTIVE election by date', () => {
    const elections = [
      el(1, 'active', '2027-02-25'),
      el(2, 'active', '2027-03-01'),
      el(3, 'completed', '2027-03-10'),
    ];
    expect(pickLatestActiveElection(elections)?.id).toBe(2);
  });

  it('falls back to the most recent election when none is active', () => {
    const elections = [el(1, 'completed', '2027-01-01'), el(2, 'completed', '2027-02-01')];
    expect(pickLatestActiveElection(elections)?.id).toBe(2);
  });

  it('matches status case-insensitively', () => {
    const elections = [el(1, 'ACTIVE', '2027-02-25'), el(2, 'draft', '2027-03-01')];
    expect(pickLatestActiveElection(elections)?.id).toBe(1);
  });
});
