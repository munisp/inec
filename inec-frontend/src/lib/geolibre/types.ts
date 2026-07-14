/**
 * INEC GeoLibre Integration Types
 *
 * Type definitions for the GeoLibre-powered geospatial layer of the INEC platform.
 * Follows GeoLibre plugin API patterns for MapLibre + deck.gl integration.
 */
import type { Feature, FeatureCollection, Point, Polygon } from 'geojson';

// ─── Election Geo Features ──────────────────────────────────────────────────

export interface PollingUnitProperties {
  code: string;
  name: string;
  ward_name: string;
  lga_name: string;
  state_name: string;
  state_code: string;
  registered_voters: number;
  status: 'finalized' | 'validated' | 'pending' | 'disputed' | 'no_result';
  total_votes_cast: number | null;
  total_valid_votes: number | null;
  turnout_pct: number | null;
  leading_party: string | null;
  leading_party_color: string | null;
  leading_party_votes: number | null;
  party_scores: PartyScore[];
  incidents_count: number;
  bvas_status: 'active' | 'inactive' | 'faulty' | 'unknown';
}

export interface PartyScore {
  party_code: string;
  abbreviation: string;
  color: string;
  votes: number;
  total_votes?: number;
}

export interface StateProperties {
  code: string;
  name: string;
  geo_zone: string;
  capital: string;
  total_pus: number;
  reported_pus: number;
  completion_pct: number;
  total_votes: number;
  total_cast: number;
  turnout_pct: number;
  leading_party: string | null;
  leading_party_color: string | null;
}

export interface IncidentProperties {
  id: number;
  type: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  description: string;
  pu_code: string;
  state_code: string;
  reported_at: string;
  status: 'open' | 'investigating' | 'resolved';
  reporter: string;
}

export interface BVASDeviceProperties {
  device_id: string;
  pu_code: string;
  status: 'active' | 'inactive' | 'faulty';
  battery_pct: number;
  accreditations: number;
  last_sync: string;
  firmware_version: string;
}

export interface OfficialTrackingProperties {
  staff_id: string;
  role: string;
  pu_code: string;
  activity: string;
  battery_pct: number;
  updated_at: string;
}

export interface GOTVCoverageProperties {
  h3_index: string;
  resolution: number;
  contact_count: number;
  volunteer_count: number;
  pledge_count: number;
  coverage_score: number;
  dominant_party: string;
}

// ─── Typed GeoJSON collections ──────────────────────────────────────────────

export type PollingUnitFeature = Feature<Point, PollingUnitProperties>;
export type PollingUnitCollection = FeatureCollection<Point, PollingUnitProperties>;

export type StateFeature = Feature<Polygon, StateProperties>;
export type StateCollection = FeatureCollection<Polygon, StateProperties>;

export type IncidentFeature = Feature<Point, IncidentProperties>;
export type IncidentCollection = FeatureCollection<Point, IncidentProperties>;

export type BVASFeature = Feature<Point, BVASDeviceProperties>;
export type BVASCollection = FeatureCollection<Point, BVASDeviceProperties>;

export type OfficialFeature = Feature<Point, OfficialTrackingProperties>;
export type OfficialCollection = FeatureCollection<Point, OfficialTrackingProperties>;

export type GOTVHexFeature = Feature<Polygon, GOTVCoverageProperties>;
export type GOTVHexCollection = FeatureCollection<Polygon, GOTVCoverageProperties>;

// ─── Layer configuration ────────────────────────────────────────────────────

export type INECLayerType =
  | 'polling-units'
  | 'state-choropleth'
  | 'incidents'
  | 'bvas-devices'
  | 'official-tracking'
  | 'gotv-hexgrid'
  | 'heatmap'
  | 'clusters'
  | 'voronoi';

export interface INECLayerConfig {
  id: INECLayerType;
  label: string;
  visible: boolean;
  opacity: number;
  data: FeatureCollection | null;
}

// ─── Spatial analysis ───────────────────────────────────────────────────────

export interface SpatialQueryResult {
  type: 'buffer' | 'intersection' | 'cluster' | 'hotspot' | 'voronoi';
  features: FeatureCollection;
  stats: Record<string, number>;
}

export interface H3AnalysisConfig {
  resolution: number;
  metric: 'turnout' | 'density' | 'coverage' | 'anomaly';
  colorScale: string[];
}

// ─── Map view state ─────────────────────────────────────────────────────────

export interface INECViewState {
  longitude: number;
  latitude: number;
  zoom: number;
  pitch: number;
  bearing: number;
}

export const NIGERIA_CENTER: INECViewState = {
  longitude: 8.0,
  latitude: 9.5,
  zoom: 5.8,
  pitch: 0,
  bearing: 0,
};

export const NIGERIA_BOUNDS: [[number, number], [number, number]] = [
  [2.5, 4.0],  // SW
  [14.7, 14.0], // NE
];
