import {
  computeClockOffsetMs, applyClockOffset, observeServerDate, correctedNowMs,
  getClockOffsetMs, resetClockSync, MAX_SANE_OFFSET_MS,
} from '../clock-sync';

describe('computeClockOffsetMs (R5-115)', () => {
  it('computes server−client offset from an HTTP Date header', () => {
    const clientNow = Date.parse('2027-02-14T12:00:00Z');
    // server 2h ahead of device
    const offset = computeClockOffsetMs('Sun, 14 Feb 2027 14:00:00 GMT', clientNow);
    expect(offset).toBe(2 * 60 * 60 * 1000);
  });

  it('returns null for missing/unparsable headers', () => {
    expect(computeClockOffsetMs(null, 1000)).toBeNull();
    expect(computeClockOffsetMs(undefined, 1000)).toBeNull();
    expect(computeClockOffsetMs('not a date', 1000)).toBeNull();
  });

  it('refuses insane offsets (> 24h) — a broken header must not corrupt timestamps', () => {
    const clientNow = Date.parse('2027-02-14T12:00:00Z');
    expect(computeClockOffsetMs('Mon, 14 Feb 2028 12:00:00 GMT', clientNow)).toBeNull();
  });

  it('accepts an offset just inside the sanity bound', () => {
    const clientNow = 0;
    const inside = new Date(MAX_SANE_OFFSET_MS - 1000).toUTCString();
    expect(computeClockOffsetMs(inside, clientNow)).toBe(MAX_SANE_OFFSET_MS - 1000);
  });
});

describe('applyClockOffset / stateful observation', () => {
  beforeEach(() => resetClockSync());

  it('applies the offset', () => {
    expect(applyClockOffset(1000, 250)).toBe(1250);
  });

  it('correctedNowMs uses the observed offset and falls back to device time', () => {
    const before = Date.now();
    expect(correctedNowMs()).toBeGreaterThanOrEqual(before); // no sample → offset 0
    const twoHours = 2 * 60 * 60 * 1000;
    observeServerDate(new Date(Date.now() + twoHours).toUTCString());
    expect(getClockOffsetMs()).toBeGreaterThan(twoHours - 5000);
    expect(correctedNowMs()).toBeGreaterThanOrEqual(before + twoHours - 5000);
  });

  it('ignores invalid samples and keeps the previous offset', () => {
    const oneHour = 60 * 60 * 1000;
    observeServerDate(new Date(Date.now() + oneHour).toUTCString());
    const observed = getClockOffsetMs();
    observeServerDate('garbage');
    expect(getClockOffsetMs()).toBe(observed);
  });
});
