/**
 * INEC deck.gl Layer Builders
 *
 * Creates deck.gl overlay layers for election data visualization.
 * Follows GeoLibre patterns: deck.gl renders on top of MapLibre GL JS.
 */
import { ScatterplotLayer, TextLayer, GeoJsonLayer } from '@deck.gl/layers';
import { HeatmapLayer } from '@deck.gl/aggregation-layers';
import { H3HexagonLayer } from '@deck.gl/geo-layers';
import type {
  PollingUnitCollection,
  IncidentCollection,
  BVASCollection,
  OfficialCollection,
  PollingUnitProperties,
  IncidentProperties,
  BVASDeviceProperties,
  OfficialTrackingProperties,
} from './types';
import type { Feature, Point } from 'geojson';

// ─── Color scales ───────────────────────────────────────────────────────────

const STATUS_COLORS: Record<string, [number, number, number, number]> = {
  finalized: [22, 163, 74, 200],
  validated: [37, 99, 235, 200],
  pending: [245, 158, 11, 200],
  disputed: [220, 38, 38, 200],
  no_result: [156, 163, 175, 120],
};

const SEVERITY_COLORS: Record<string, [number, number, number, number]> = {
  low: [59, 130, 246, 180],
  medium: [245, 158, 11, 200],
  high: [249, 115, 22, 220],
  critical: [220, 38, 38, 255],
};

const BVAS_COLORS: Record<string, [number, number, number, number]> = {
  active: [22, 163, 74, 200],
  inactive: [156, 163, 175, 150],
  faulty: [220, 38, 38, 200],
  unknown: [107, 114, 128, 120],
};

function turnoutToColor(pct: number | null): [number, number, number, number] {
  if (pct === null) return [156, 163, 175, 100];
  if (pct >= 70) return [22, 163, 74, 220];
  if (pct >= 50) return [59, 130, 246, 200];
  if (pct >= 30) return [245, 158, 11, 180];
  return [220, 38, 38, 160];
}

function coverageToColor(score: number): [number, number, number, number] {
  if (score >= 80) return [22, 163, 74, 160];
  if (score >= 60) return [59, 130, 246, 140];
  if (score >= 40) return [245, 158, 11, 120];
  if (score >= 20) return [249, 115, 22, 100];
  return [220, 38, 38, 80];
}

// ─── Polling Unit Layers ────────────────────────────────────────────────────

type PUFeature = Feature<Point, PollingUnitProperties>;

export function createPollingUnitScatterLayer(
  data: PollingUnitCollection,
  mode: 'status' | 'turnout' | 'party' = 'status',
  onClick?: (info: { object: PUFeature }) => void,
) {
  return new ScatterplotLayer<PUFeature>({
    id: 'inec-polling-units',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getRadius: (d) => {
      const voters = d.properties.registered_voters;
      return Math.max(50, Math.min(500, Math.sqrt(voters) * 3));
    },
    getFillColor: (d) => {
      if (mode === 'status') return STATUS_COLORS[d.properties.status] || STATUS_COLORS.no_result;
      if (mode === 'turnout') return turnoutToColor(d.properties.turnout_pct);
      if (mode === 'party' && d.properties.leading_party_color) {
        const hex = d.properties.leading_party_color;
        const r = parseInt(hex.slice(1, 3), 16);
        const g = parseInt(hex.slice(3, 5), 16);
        const b = parseInt(hex.slice(5, 7), 16);
        return [r, g, b, 200];
      }
      return STATUS_COLORS.no_result;
    },
    getLineColor: [255, 255, 255, 100],
    lineWidthMinPixels: 1,
    radiusMinPixels: 3,
    radiusMaxPixels: 20,
    pickable: true,
    onClick: onClick as never,
    updateTriggers: {
      getFillColor: [mode],
    },
  });
}

export function createPollingUnitLabelsLayer(
  data: PollingUnitCollection,
  _minZoom = 10,
) {
  return new TextLayer<PUFeature>({
    id: 'inec-pu-labels',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getText: (d) => d.properties.name.length > 25 ? d.properties.name.slice(0, 22) + '...' : d.properties.name,
    getSize: 12,
    getColor: [30, 30, 30, 255],
    getAngle: 0,
    getTextAnchor: 'middle',
    getAlignmentBaseline: 'top',
    getPixelOffset: [0, 12],
    fontFamily: 'Inter, sans-serif',
    fontWeight: 500,
    outlineWidth: 2,
    outlineColor: [255, 255, 255, 200],
    visible: true,
  });
}

// ─── Heatmap Layer ──────────────────────────────────────────────────────────

export function createTurnoutHeatmapLayer(
  data: PollingUnitCollection,
  metric: 'turnout' | 'density' | 'votes' = 'turnout',
) {
  return new HeatmapLayer<PUFeature>({
    id: 'inec-heatmap',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getWeight: (d) => {
      if (metric === 'turnout') return d.properties.turnout_pct || 0;
      if (metric === 'density') return d.properties.registered_voters;
      return d.properties.total_votes_cast || 0;
    },
    radiusPixels: 60,
    intensity: 1.5,
    threshold: 0.05,
    colorRange: [
      [255, 255, 178],
      [254, 204, 92],
      [253, 141, 60],
      [240, 59, 32],
      [189, 0, 38],
    ],
  });
}

// ─── Incident Layer ─────────────────────────────────────────────────────────

type IncFeature = Feature<Point, IncidentProperties>;

export function createIncidentLayer(
  data: IncidentCollection,
  onClick?: (info: { object: IncFeature }) => void,
) {
  return new ScatterplotLayer<IncFeature>({
    id: 'inec-incidents',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getRadius: (d) => {
      const sizes: Record<string, number> = { low: 200, medium: 350, high: 500, critical: 700 };
      return sizes[d.properties.severity] || 300;
    },
    getFillColor: (d) => SEVERITY_COLORS[d.properties.severity] || SEVERITY_COLORS.medium,
    getLineColor: [255, 255, 255, 200],
    lineWidthMinPixels: 2,
    stroked: true,
    radiusMinPixels: 6,
    radiusMaxPixels: 30,
    pickable: true,
    onClick: onClick as never,
  });
}

// ─── BVAS Device Layer ──────────────────────────────────────────────────────

type BVASFeature = Feature<Point, BVASDeviceProperties>;

export function createBVASLayer(
  data: BVASCollection,
  onClick?: (info: { object: BVASFeature }) => void,
) {
  return new ScatterplotLayer<BVASFeature>({
    id: 'inec-bvas',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getRadius: 150,
    getFillColor: (d) => BVAS_COLORS[d.properties.status] || BVAS_COLORS.unknown,
    getLineColor: (d) => {
      if (d.properties.battery_pct < 20) return [220, 38, 38, 255] as [number, number, number, number];
      return [255, 255, 255, 150] as [number, number, number, number];
    },
    lineWidthMinPixels: 2,
    stroked: true,
    radiusMinPixels: 5,
    radiusMaxPixels: 15,
    pickable: true,
    onClick: onClick as never,
  });
}

// ─── Official Tracking Layer ────────────────────────────────────────────────

type OffFeature = Feature<Point, OfficialTrackingProperties>;

export function createOfficialTrackingLayer(
  data: OfficialCollection,
  onClick?: (info: { object: OffFeature }) => void,
) {
  return new ScatterplotLayer<OffFeature>({
    id: 'inec-officials',
    data: data.features,
    getPosition: (d) => d.geometry.coordinates as [number, number],
    getRadius: 120,
    getFillColor: [37, 99, 235, 200],
    getLineColor: [255, 255, 255, 200],
    lineWidthMinPixels: 2,
    stroked: true,
    radiusMinPixels: 5,
    radiusMaxPixels: 15,
    pickable: true,
    onClick: onClick as never,
  });
}

// ─── GOTV H3 Hex Coverage Layer ─────────────────────────────────────────────

export function createGOTVHexLayer(
  hexData: Array<{ h3_index: string; coverage_score: number; contact_count: number; volunteer_count: number }>,
) {
  return new H3HexagonLayer({
    id: 'inec-gotv-h3',
    data: hexData,
    getHexagon: (d: { h3_index: string }) => d.h3_index,
    getFillColor: (d: { coverage_score: number }) => coverageToColor(d.coverage_score),
    getElevation: (d: { contact_count: number }) => d.contact_count * 10,
    elevationScale: 50,
    extruded: true,
    opacity: 0.6,
    pickable: true,
  });
}

// ─── GeoJSON Overlay Layer (for spatial analysis results) ───────────────────

export function createAnalysisResultLayer(
  data: GeoJSON.FeatureCollection,
  id = 'inec-analysis',
  color: [number, number, number, number] = [124, 58, 237, 120],
) {
  return new GeoJsonLayer({
    id,
    data,
    getFillColor: color,
    getLineColor: [124, 58, 237, 200],
    lineWidthMinPixels: 2,
    pickable: true,
    stroked: true,
    filled: true,
    opacity: 0.5,
  });
}
