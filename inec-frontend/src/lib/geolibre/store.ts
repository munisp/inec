/**
 * INEC GeoLibre Map Store
 *
 * Zustand store managing all geospatial state for the GeoLibre-powered map.
 * Follows GeoLibre's store-driven architecture pattern.
 */
import { create } from 'zustand';
import type {
  PollingUnitCollection,
  StateCollection,
  IncidentCollection,
  BVASCollection,
  OfficialCollection,
  INECViewState,
  INECLayerType,
} from './types';
import { NIGERIA_CENTER } from './types';

interface GeoLibreMapState {
  // View state
  viewState: INECViewState;
  setViewState: (vs: Partial<INECViewState>) => void;

  // Election context
  electionId: number;
  setElectionId: (id: number) => void;
  selectedStateCode: string | null;
  setSelectedStateCode: (code: string | null) => void;

  // Data collections
  pollingUnits: PollingUnitCollection;
  states: StateCollection;
  incidents: IncidentCollection;
  bvasDevices: BVASCollection;
  officials: OfficialCollection;
  setPollingUnits: (data: PollingUnitCollection) => void;
  setStates: (data: StateCollection) => void;
  setIncidents: (data: IncidentCollection) => void;
  setBvasDevices: (data: BVASCollection) => void;
  setOfficials: (data: OfficialCollection) => void;

  // Layer visibility
  visibleLayers: Set<INECLayerType>;
  toggleLayer: (layer: INECLayerType) => void;
  setLayerVisible: (layer: INECLayerType, visible: boolean) => void;

  // Visualization mode
  colorMode: 'status' | 'turnout' | 'party';
  setColorMode: (mode: 'status' | 'turnout' | 'party') => void;
  heatmapMetric: 'turnout' | 'density' | 'votes';
  setHeatmapMetric: (metric: 'turnout' | 'density' | 'votes') => void;

  // Basemap
  basemapStyle: string;
  setBasemapStyle: (style: string) => void;

  // 3D mode
  is3D: boolean;
  toggle3D: () => void;

  // Selection
  selectedFeatureId: string | null;
  setSelectedFeatureId: (id: string | null) => void;

  // Loading
  loading: boolean;
  setLoading: (loading: boolean) => void;

  // GeoLibre viewer iframe
  geolibreViewerUrl: string | null;
  setGeolibreViewerUrl: (url: string | null) => void;

  // Spatial analysis results
  analysisResult: GeoJSON.FeatureCollection | null;
  setAnalysisResult: (data: GeoJSON.FeatureCollection | null) => void;
}

const EMPTY_FC = { type: 'FeatureCollection' as const, features: [] };

export const useGeoLibreStore = create<GeoLibreMapState>((set) => ({
  viewState: NIGERIA_CENTER,
  setViewState: (vs) => set((s) => ({ viewState: { ...s.viewState, ...vs } })),

  electionId: 1,
  setElectionId: (id) => set({ electionId: id }),
  selectedStateCode: null,
  setSelectedStateCode: (code) => set({ selectedStateCode: code }),

  pollingUnits: EMPTY_FC,
  states: EMPTY_FC,
  incidents: EMPTY_FC,
  bvasDevices: EMPTY_FC,
  officials: EMPTY_FC,
  setPollingUnits: (data) => set({ pollingUnits: data }),
  setStates: (data) => set({ states: data }),
  setIncidents: (data) => set({ incidents: data }),
  setBvasDevices: (data) => set({ bvasDevices: data }),
  setOfficials: (data) => set({ officials: data }),

  visibleLayers: new Set<INECLayerType>(['polling-units', 'state-choropleth']),
  toggleLayer: (layer) => set((s) => {
    const next = new Set(s.visibleLayers);
    if (next.has(layer)) next.delete(layer); else next.add(layer);
    return { visibleLayers: next };
  }),
  setLayerVisible: (layer, visible) => set((s) => {
    const next = new Set(s.visibleLayers);
    if (visible) next.add(layer); else next.delete(layer);
    return { visibleLayers: next };
  }),

  colorMode: 'status',
  setColorMode: (mode) => set({ colorMode: mode }),
  heatmapMetric: 'turnout',
  setHeatmapMetric: (metric) => set({ heatmapMetric: metric }),

  basemapStyle: 'https://tiles.openfreemap.org/styles/liberty',
  setBasemapStyle: (style) => set({ basemapStyle: style }),

  is3D: false,
  toggle3D: () => set((s) => ({ is3D: !s.is3D, viewState: { ...s.viewState, pitch: s.is3D ? 0 : 45 } })),

  selectedFeatureId: null,
  setSelectedFeatureId: (id) => set({ selectedFeatureId: id }),

  loading: false,
  setLoading: (loading) => set({ loading }),

  geolibreViewerUrl: null,
  setGeolibreViewerUrl: (url) => set({ geolibreViewerUrl: url }),

  analysisResult: null,
  setAnalysisResult: (data) => set({ analysisResult: data }),
}));
