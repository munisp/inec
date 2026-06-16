/**
 * INEC GeoLibre Data Provider
 *
 * Fetches election data from the INEC backend and converts it to GeoJSON
 * for consumption by GeoLibre map layers and deck.gl overlays.
 */
import type {
  PollingUnitCollection,
  StateCollection,
  IncidentCollection,
  BVASCollection,
  OfficialCollection,
  GOTVHexCollection,
  PartyScore,
} from './types';
import { NIGERIA_STATE_COORDS } from '@/lib/nigeria-geo';

const API_BASE = import.meta.env.VITE_API_URL || '';

async function apiFetch(path: string): Promise<unknown> {
  const token = localStorage.getItem('auth_token') || '';
  const res = await fetch(`${API_BASE}${path}`, {
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${path}`);
  return res.json();
}

/**
 * Fetch all polling units as GeoJSON FeatureCollection
 */
export async function fetchPollingUnitsGeoJSON(
  electionId: number,
  stateCode?: string,
): Promise<PollingUnitCollection> {
  const params = new URLSearchParams();
  params.set('election_id', String(electionId));
  if (stateCode) params.set('state_code', stateCode);
  params.set('limit', '50000');

  try {
    const data = await apiFetch(`/geo/map-data?${params}`) as {
      polling_units?: Array<{
        code: string; name: string; latitude: number; longitude: number;
        registered_voters: number; ward_name: string; lga_name: string;
        state_name: string; state_code: string;
        result_id: number | null; status: string | null;
        total_valid_votes: number | null; total_votes_cast: number | null;
        party_scores?: PartyScore[];
      }>;
    };

    const pus = data.polling_units || [];
    return {
      type: 'FeatureCollection',
      features: pus
        .filter(pu => pu.latitude && pu.longitude)
        .map(pu => {
          const turnout = pu.registered_voters > 0 && pu.total_votes_cast
            ? (pu.total_votes_cast / pu.registered_voters) * 100 : null;
          const scores = pu.party_scores || [];
          const leading = scores.length > 0
            ? scores.reduce((a, b) => a.votes > b.votes ? a : b) : null;

          return {
            type: 'Feature' as const,
            geometry: { type: 'Point' as const, coordinates: [pu.longitude, pu.latitude] },
            properties: {
              code: pu.code,
              name: pu.name,
              ward_name: pu.ward_name,
              lga_name: pu.lga_name,
              state_name: pu.state_name,
              state_code: pu.state_code,
              registered_voters: pu.registered_voters,
              status: (pu.status || 'no_result') as 'finalized' | 'validated' | 'pending' | 'disputed' | 'no_result',
              total_votes_cast: pu.total_votes_cast,
              total_valid_votes: pu.total_valid_votes,
              turnout_pct: turnout,
              leading_party: leading?.abbreviation || null,
              leading_party_color: leading?.color || null,
              leading_party_votes: leading?.votes || null,
              party_scores: scores,
              incidents_count: 0,
              bvas_status: 'unknown' as const,
            },
          };
        }),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Fetch state-level data as polygon GeoJSON (approximate boundaries from centroids)
 */
export async function fetchStatesGeoJSON(electionId: number): Promise<StateCollection> {
  try {
    const data = await apiFetch(`/geo/map-data?election_id=${electionId}`) as {
      states?: Array<{
        code: string; name: string; geo_zone: string; capital: string;
        total_pus: number; reported_pus: number;
        total_votes: number; total_cast: number;
        party_scores?: PartyScore[];
      }>;
    };

    const states = data.states || [];
    return {
      type: 'FeatureCollection',
      features: states
        .filter(s => NIGERIA_STATE_COORDS[s.code])
        .map(s => {
          const coords = NIGERIA_STATE_COORDS[s.code];
          const scores = s.party_scores || [];
          const leading = scores.length > 0
            ? scores.reduce((a, b) => a.total_votes > b.total_votes ? a : b) : null;
          const d = 0.5; // approximate polygon size

          return {
            type: 'Feature' as const,
            geometry: {
              type: 'Polygon' as const,
              coordinates: [[
                [coords.lng - d, coords.lat - d],
                [coords.lng + d, coords.lat - d],
                [coords.lng + d, coords.lat + d],
                [coords.lng - d, coords.lat + d],
                [coords.lng - d, coords.lat - d],
              ]],
            },
            properties: {
              code: s.code,
              name: s.name,
              geo_zone: s.geo_zone,
              capital: s.capital,
              total_pus: s.total_pus,
              reported_pus: s.reported_pus,
              completion_pct: s.total_pus > 0 ? (s.reported_pus / s.total_pus) * 100 : 0,
              total_votes: s.total_votes,
              total_cast: s.total_cast,
              turnout_pct: s.total_votes > 0 ? (s.total_cast / s.total_votes) * 100 : 0,
              leading_party: leading?.abbreviation || null,
              leading_party_color: leading?.color || null,
            },
          };
        }),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Fetch incidents as GeoJSON
 */
export async function fetchIncidentsGeoJSON(electionId: number): Promise<IncidentCollection> {
  try {
    const data = await apiFetch(`/incidents?election_id=${electionId}`) as {
      incidents?: Array<{
        id: number; incident_type: string; severity: string; description: string;
        polling_unit_code: string; state_code: string; latitude: number; longitude: number;
        reported_at: string; status: string; reporter_name: string;
      }>;
    };

    const incidents = data.incidents || [];
    return {
      type: 'FeatureCollection',
      features: incidents
        .filter(i => i.latitude && i.longitude)
        .map(i => ({
          type: 'Feature' as const,
          geometry: { type: 'Point' as const, coordinates: [i.longitude, i.latitude] },
          properties: {
            id: i.id,
            type: i.incident_type,
            severity: (i.severity || 'medium') as 'low' | 'medium' | 'high' | 'critical',
            description: i.description,
            pu_code: i.polling_unit_code,
            state_code: i.state_code,
            reported_at: i.reported_at,
            status: (i.status || 'open') as 'open' | 'investigating' | 'resolved',
            reporter: i.reporter_name,
          },
        })),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Fetch BVAS device locations
 */
export async function fetchBVASGeoJSON(): Promise<BVASCollection> {
  try {
    const data = await apiFetch('/bvas/devices') as {
      devices?: Array<{
        device_id: string; polling_unit_code: string; status: string;
        battery_level: number; accreditation_count: number;
        latitude: number; longitude: number;
        last_sync_at: string; firmware_version: string;
      }>;
    };

    const devices = data.devices || [];
    return {
      type: 'FeatureCollection',
      features: devices
        .filter(d => d.latitude && d.longitude)
        .map(d => ({
          type: 'Feature' as const,
          geometry: { type: 'Point' as const, coordinates: [d.longitude, d.latitude] },
          properties: {
            device_id: d.device_id,
            pu_code: d.polling_unit_code,
            status: (d.status || 'unknown') as 'active' | 'inactive' | 'faulty',
            battery_pct: d.battery_level,
            accreditations: d.accreditation_count,
            last_sync: d.last_sync_at,
            firmware_version: d.firmware_version,
          },
        })),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Fetch official tracking positions
 */
export async function fetchOfficialsGeoJSON(): Promise<OfficialCollection> {
  try {
    const data = await apiFetch('/geo/tracking/officials') as {
      officials?: Array<{
        staff_id: string; role: string; latitude: number; longitude: number;
        pu_code: string; activity: string; battery_pct: number; updated_at: string;
      }>;
    };

    const officials = data.officials || [];
    return {
      type: 'FeatureCollection',
      features: officials
        .filter(o => o.latitude && o.longitude)
        .map(o => ({
          type: 'Feature' as const,
          geometry: { type: 'Point' as const, coordinates: [o.longitude, o.latitude] },
          properties: {
            staff_id: o.staff_id,
            role: o.role,
            pu_code: o.pu_code,
            activity: o.activity,
            battery_pct: o.battery_pct,
            updated_at: o.updated_at,
          },
        })),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Fetch GOTV H3 hex grid coverage data
 */
export async function fetchGOTVHexGeoJSON(resolution = 5): Promise<GOTVHexCollection> {
  try {
    const data = await apiFetch(`/gotv/geo/h3?resolution=${resolution}`) as {
      hexagons?: Array<{
        h3_index: string;
        contact_count: number;
        volunteer_count: number;
        pledge_count: number;
        coverage_score: number;
        dominant_party: string;
        boundary: number[][];
      }>;
    };

    const hexagons = data.hexagons || [];
    return {
      type: 'FeatureCollection',
      features: hexagons
        .filter(h => h.boundary && h.boundary.length > 0)
        .map(h => ({
          type: 'Feature' as const,
          geometry: {
            type: 'Polygon' as const,
            coordinates: [h.boundary.map(c => [c[1], c[0]])],
          },
          properties: {
            h3_index: h.h3_index,
            resolution,
            contact_count: h.contact_count,
            volunteer_count: h.volunteer_count,
            pledge_count: h.pledge_count,
            coverage_score: h.coverage_score,
            dominant_party: h.dominant_party,
          },
        })),
    };
  } catch {
    return { type: 'FeatureCollection', features: [] };
  }
}

/**
 * Export all election data as a downloadable GeoJSON file
 */
export function downloadGeoJSON(data: unknown, filename: string): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/geo+json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
