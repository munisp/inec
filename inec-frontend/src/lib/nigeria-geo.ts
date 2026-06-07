export const NIGERIA_STATE_COORDS: Record<string, { lat: number; lng: number }> = {
  AB: { lat: 5.45, lng: 7.52 },
  AD: { lat: 9.33, lng: 12.40 },
  AK: { lat: 5.01, lng: 7.85 },
  AN: { lat: 6.21, lng: 6.94 },
  BA: { lat: 10.31, lng: 9.84 },
  BY: { lat: 4.77, lng: 6.07 },
  BE: { lat: 7.34, lng: 8.77 },
  BO: { lat: 11.85, lng: 13.15 },
  CR: { lat: 5.87, lng: 8.53 },
  DE: { lat: 5.89, lng: 5.68 },
  EB: { lat: 6.26, lng: 8.07 },
  ED: { lat: 6.63, lng: 5.93 },
  EK: { lat: 7.72, lng: 5.31 },
  EN: { lat: 6.45, lng: 7.50 },
  FC: { lat: 9.06, lng: 7.49 },
  GO: { lat: 10.29, lng: 11.17 },
  IM: { lat: 5.57, lng: 7.06 },
  JI: { lat: 12.23, lng: 9.56 },
  KD: { lat: 10.52, lng: 7.43 },
  KN: { lat: 12.00, lng: 8.52 },
  KT: { lat: 12.99, lng: 7.60 },
  KE: { lat: 12.45, lng: 4.20 },
  KO: { lat: 7.80, lng: 6.74 },
  KW: { lat: 8.50, lng: 4.55 },
  LA: { lat: 6.60, lng: 3.35 },
  NA: { lat: 8.54, lng: 8.52 },
  NI: { lat: 9.93, lng: 5.60 },
  OG: { lat: 7.16, lng: 3.35 },
  ON: { lat: 7.25, lng: 5.19 },
  OS: { lat: 7.77, lng: 4.57 },
  OY: { lat: 7.85, lng: 3.93 },
  PL: { lat: 9.22, lng: 9.52 },
  RI: { lat: 4.82, lng: 7.03 },
  SO: { lat: 13.06, lng: 5.24 },
  TA: { lat: 8.89, lng: 11.36 },
  YO: { lat: 12.00, lng: 11.50 },
  ZA: { lat: 12.17, lng: 6.66 },
};

export function generateStateBoundaryGeoJSON(
  states: Array<{
    code: string;
    name: string;
    geo_zone: string;
    total_votes: number;
    reported_pus: number;
    total_pus: number;
    leading_party: { abbreviation: string; color: string; total_votes: number } | null;
  }>
) {
  const features = states.map((state) => {
    const center = NIGERIA_STATE_COORDS[state.code];
    if (!center) return null;

    const r = state.code === 'LA' ? 0.25 : state.code === 'FC' ? 0.3 : 0.55;
    const sides = 6;
    const coords = [];
    for (let i = 0; i <= sides; i++) {
      const angle = (Math.PI * 2 * i) / sides - Math.PI / 6;
      coords.push([
        center.lng + r * Math.cos(angle) * (0.9 + Math.random() * 0.2),
        center.lat + r * Math.sin(angle) * (0.9 + Math.random() * 0.2),
      ]);
    }

    return {
      type: 'Feature' as const,
      properties: {
        code: state.code,
        name: state.name,
        geo_zone: state.geo_zone,
        total_votes: state.total_votes,
        reported_pus: state.reported_pus,
        total_pus: state.total_pus,
        completion: state.total_pus > 0 ? Math.round((state.reported_pus / state.total_pus) * 100) : 0,
        leading_party: state.leading_party?.abbreviation || 'N/A',
        leading_color: state.leading_party?.color || '#cccccc',
        leading_votes: state.leading_party?.total_votes || 0,
      },
      geometry: {
        type: 'Polygon' as const,
        coordinates: [coords],
      },
    };
  });

  return {
    type: 'FeatureCollection' as const,
    features: features.filter(Boolean),
  };
}

export function generatePUGeoJSON(
  pollingUnits: Array<{
    code: string;
    name: string;
    latitude: number;
    longitude: number;
    registered_voters: number;
    state_name: string;
    lga_name: string;
    ward_name: string;
    status: string | null;
    total_valid_votes: number | null;
    total_votes_cast: number | null;
    tigerbeetle_status: string | null;
    hyperledger_status: string | null;
    party_scores: Array<{ abbreviation: string; color: string; votes: number }>;
  }>
) {
  const features = pollingUnits.map((pu) => ({
    type: 'Feature' as const,
    properties: {
      code: pu.code,
      name: pu.name,
      state: pu.state_name,
      lga: pu.lga_name,
      ward: pu.ward_name,
      registered: pu.registered_voters || 0,
      status: pu.status || 'no_result',
      votes: pu.total_valid_votes || 0,
      cast: pu.total_votes_cast || 0,
      tb: pu.tigerbeetle_status || 'N/A',
      hl: pu.hyperledger_status || 'N/A',
      topParty: pu.party_scores?.[0]?.abbreviation || 'N/A',
      topColor: pu.party_scores?.[0]?.color || '#888888',
      topVotes: pu.party_scores?.[0]?.votes || 0,
    },
    geometry: {
      type: 'Point' as const,
      coordinates: [pu.longitude, pu.latitude],
    },
  }));

  return {
    type: 'FeatureCollection' as const,
    features,
  };
}

export const ZONE_COLORS: Record<string, string> = {
  'North Central': '#2563eb',
  'North East': '#dc2626',
  'North West': '#16a34a',
  'South East': '#9333ea',
  'South South': '#ea580c',
  'South West': '#0891b2',
};
