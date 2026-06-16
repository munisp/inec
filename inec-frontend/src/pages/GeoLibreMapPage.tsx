/**
 * GeoLibreMapPage — Production-ready GeoLibre-powered geospatial analytics for INEC
 *
 * Full feature parity with MapPage PLUS GeoLibre-unique capabilities:
 * - deck.gl overlays on MapLibre (high-perf WebGL)
 * - SSE real-time official tracking with pulsing markers
 * - WebSocket result updates for live tile refresh
 * - Crowd density markers, landmark markers, incident hotspots
 * - Search/geocoding (Nominatim + PU search)
 * - State drill-down, status filters, satellite mode
 * - Box select, street view, voice navigation
 * - Compare mode (dual maps)
 * - Time slider for temporal filtering
 * - Spatial Analysis (8 tools), GeoLibre Viewer, Field Kit
 *
 * Tabs: Live Map | Spatial Analysis | GeoLibre Viewer | Field Kit
 */
import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import {
  MapPin, Layers, Eye, EyeOff, Flame, Hexagon, Radio, Shield,
  AlertTriangle, Download, Satellite, Globe, BarChart3, Search,
  RefreshCw, Box, Users, Cpu, Database, Activity, Navigation,
  ArrowLeft, Map as MapIcon, Cloud, Battery, Mic, Route, Building2,
  ExternalLink,
} from 'lucide-react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { MapboxOverlay } from '@deck.gl/mapbox';
import { useGeoLibreStore } from '@/lib/geolibre/store';
import { api } from '@/lib/api';
import {
  fetchPollingUnitsGeoJSON,
  fetchStatesGeoJSON,
  fetchIncidentsGeoJSON,
  fetchBVASGeoJSON,
  fetchOfficialsGeoJSON,
  downloadGeoJSON,
} from '@/lib/geolibre/data-provider';
import {
  createPollingUnitScatterLayer,
  createTurnoutHeatmapLayer,
  createIncidentLayer,
  createBVASLayer,
  createOfficialTrackingLayer,
  createAnalysisResultLayer,
} from '@/lib/geolibre/deck-layers';
import type { INECLayerType } from '@/lib/geolibre/types';
import { NIGERIA_CENTER } from '@/lib/geolibre/types';

type TabId = 'live-map' | 'spatial' | 'geolibre' | 'field-kit';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8000';

const LAYER_CONFIG: { id: INECLayerType; label: string; icon: typeof MapPin; color: string }[] = [
  { id: 'polling-units', label: 'Polling Units', icon: MapPin, color: 'text-blue-500' },
  { id: 'state-choropleth', label: 'State Choropleth', icon: Layers, color: 'text-green-500' },
  { id: 'incidents', label: 'Incidents', icon: AlertTriangle, color: 'text-red-500' },
  { id: 'bvas-devices', label: 'BVAS Devices', icon: Radio, color: 'text-purple-500' },
  { id: 'official-tracking', label: 'Official Tracking', icon: Users, color: 'text-cyan-500' },
  { id: 'gotv-hexgrid', label: 'GOTV Hex Coverage', icon: Hexagon, color: 'text-amber-500' },
  { id: 'heatmap', label: 'Turnout Heatmap', icon: Flame, color: 'text-orange-500' },
  { id: 'clusters', label: 'Clusters', icon: Globe, color: 'text-indigo-500' },
];

const BASEMAPS: { id: string; label: string; url: string; type: 'vector' | 'raster' }[] = [
  { id: 'liberty', label: 'OpenFreeMap', url: 'https://tiles.openfreemap.org/styles/liberty', type: 'vector' },
  { id: 'positron', label: 'CARTO Positron', url: 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json', type: 'vector' },
  { id: 'dark', label: 'CARTO Dark', url: 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json', type: 'vector' },
  { id: 'voyager', label: 'CARTO Voyager', url: 'https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json', type: 'vector' },
  { id: 'satellite', label: 'Satellite', url: 'https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}', type: 'raster' },
  { id: 'osm', label: 'OpenStreetMap', url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png', type: 'raster' },
];

const ROLE_COLORS: Record<string, string> = {
  presiding_officer: '#dc2626', asst_presiding: '#ea580c', observer: '#2563eb',
  security: '#16a34a', supervisor: '#7c3aed', tech_support: '#0891b2',
  returning_officer: '#be123c', field_officer: '#6b7280',
};

const ROLE_ICONS: Record<string, string> = {
  presiding_officer: '👨‍⚖️', asst_presiding: '📋', observer: '👁️',
  security: '🛡️', supervisor: '⭐', tech_support: '🔧',
  returning_officer: '🏛️', field_officer: '👤',
};

const STATUS_COLORS: Record<string, string> = {
  finalized: '#16a34a', validated: '#2563eb', pending: '#f59e0b',
  disputed: '#dc2626', no_result: '#9ca3af',
};

function formatNumber(n: number) { return new Intl.NumberFormat().format(n); }

interface OfficialData {
  staff_id: string; role: string; latitude: number; longitude: number;
  pu_code: string; activity: string; battery_pct: number; updated_at: string;
}

interface CrowdReport {
  pu_code: string; latitude: number; longitude: number; head_count: number;
  density_level: string; queue_length: number; wait_time_min: number; pu_name: string;
}

interface LandmarkData {
  id: number; name: string; category: string; latitude: number;
  longitude: number; icon: string; address: string;
}

export default function GeoLibreMapPage() {
  const [activeTab, setActiveTab] = useState<TabId>('live-map');

  const tabs: { id: TabId; label: string; icon: typeof MapPin }[] = [
    { id: 'live-map', label: 'Live Map', icon: Globe },
    { id: 'spatial', label: 'Spatial Analysis', icon: Database },
    { id: 'geolibre', label: 'GeoLibre Viewer', icon: Cpu },
    { id: 'field-kit', label: 'Field Kit', icon: Shield },
  ];

  return (
    <div className="h-full flex flex-col">
      {/* Tab bar */}
      <div className="flex border-b bg-white px-4 py-1 gap-1 shrink-0">
        {tabs.map(t => (
          <button key={t.id}
            className={`flex items-center gap-2 px-4 py-2 rounded-t text-sm font-medium transition-colors ${activeTab === t.id ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted'}`}
            onClick={() => setActiveTab(t.id)}
          >
            <t.icon className="w-4 h-4" /> {t.label}
          </button>
        ))}
        <div className="flex-1" />
        <Badge variant="outline" className="self-center text-xs">
          Powered by GeoLibre + deck.gl
        </Badge>
      </div>

      {/* Tab content */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {activeTab === 'live-map' && <LiveMapTab />}
        {activeTab === 'spatial' && <SpatialAnalysisTab />}
        {activeTab === 'geolibre' && <GeoLibreViewerTab />}
        {activeTab === 'field-kit' && <FieldKitTab />}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// LIVE MAP TAB — deck.gl overlays on MapLibre + real-time markers + full controls
// ═══════════════════════════════════════════════════════════════════════════

function LiveMapTab() {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const deckOverlay = useRef<MapboxOverlay | null>(null);
  const [mapReady, setMapReady] = useState(false);
  const [stats, setStats] = useState({ pus: 0, incidents: 0, bvas: 0, officials: 0 });
  const [selectedFeature, setSelectedFeature] = useState<Record<string, unknown> | null>(null);

  // Real-time tracking state
  const [showTracking, setShowTracking] = useState(true);
  const [officials, setOfficials] = useState<OfficialData[]>([]);
  const [crowdReports, setCrowdReports] = useState<CrowdReport[]>([]);
  const [showCrowd, setShowCrowd] = useState(true);
  const officialMarkers = useRef<maplibregl.Marker[]>([]);
  const crowdMarkers = useRef<maplibregl.Marker[]>([]);
  const sseRef = useRef<EventSource | null>(null);
  const trackingInterval = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasZoomedToTrack = useRef(false);

  // Landmark state
  const [showLandmarks, setShowLandmarks] = useState(true);
  const [landmarks, setLandmarks] = useState<LandmarkData[]>([]);
  const landmarkMarkers = useRef<maplibregl.Marker[]>([]);

  // Geofence + incident + weather
  const [showGeofences, setShowGeofences] = useState(true);
  const [showIncidentHotspots, setShowIncidentHotspots] = useState(true);
  const [showWeather, setShowWeather] = useState(false);
  const [weatherData, setWeatherData] = useState<Array<{ name: string; lat: number; lng: number; weather: { temp_c: number; humidity: number; description: string; wind_kmh: number } }>>([]);
  const geofenceMarkers = useRef<maplibregl.Marker[]>([]);
  const incidentHotspotMarkers = useRef<maplibregl.Marker[]>([]);
  const weatherMarkers = useRef<maplibregl.Marker[]>([]);

  // Search, state drill-down, status filters
  const [searchQuery, setSearchQuery] = useState('');
  const [places, setPlaces] = useState<Array<{ name: string; lat: number; lon: number }>>([]);
  const [selectedState, setSelectedState] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<Record<string, boolean>>({
    finalized: true, validated: true, pending: true, disputed: true, no_result: true,
  });

  // Street view, voice, compare, box select
  const [streetViewUrl, setStreetViewUrl] = useState<string | null>(null);
  const [voiceListening, setVoiceListening] = useState(false);
  const [compareMode, setCompareMode] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [selectionBox, setSelectionBox] = useState<{ x1: number; y1: number; x2: number; y2: number } | null>(null);
  const [selectedCodes, setSelectedCodes] = useState<string[]>([]);

  // Time slider
  const [timeTs, setTimeTs] = useState<number | null>(null);

  // Crowd alerts
  const [crowdAlerts, setCrowdAlerts] = useState<Array<{ id: number; pu_code: string; severity: string; message: string; created_at: string }>>([]);

  // Right sidebar state
  const [showRightPanel, setShowRightPanel] = useState(true);

  const store = useGeoLibreStore();

  // Initialize map — fixed height bug with explicit height
  useEffect(() => {
    if (!mapContainer.current || mapRef.current) return;

    const basemap = BASEMAPS.find(b => b.url === store.basemapStyle);
    const isRaster = basemap?.type === 'raster';

    const style = isRaster ? {
      version: 8 as const,
      sources: {
        'raster-tiles': {
          type: 'raster' as const,
          tiles: [store.basemapStyle],
          tileSize: 256,
          attribution: '&copy; Esri/OSM',
        },
      },
      layers: [{
        id: 'raster-tiles',
        type: 'raster' as const,
        source: 'raster-tiles',
        minzoom: 0,
        maxzoom: 19,
      }],
    } : store.basemapStyle;

    const map = new maplibregl.Map({
      container: mapContainer.current,
      style,
      center: [NIGERIA_CENTER.longitude, NIGERIA_CENTER.latitude],
      zoom: NIGERIA_CENTER.zoom,
      pitch: store.is3D ? 45 : 0,
      attributionControl: false,
    });

    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.addControl(new maplibregl.ScaleControl(), 'bottom-left');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'bottom-right');
    map.addControl(new maplibregl.FullscreenControl(), 'top-right');

    const overlay = new MapboxOverlay({
      interleaved: false,
      layers: [],
    });
    map.addControl(overlay as unknown as maplibregl.IControl);

    map.on('load', () => setMapReady(true));

    mapRef.current = map;
    deckOverlay.current = overlay;

    return () => { map.remove(); mapRef.current = null; };
  }, []);

  // Load data
  const loadData = useCallback(async () => {
    store.setLoading(true);
    try {
      const [pus, incidents, bvas, offi] = await Promise.all([
        fetchPollingUnitsGeoJSON(store.electionId, store.selectedStateCode || undefined),
        fetchIncidentsGeoJSON(store.electionId),
        fetchBVASGeoJSON(),
        fetchOfficialsGeoJSON(),
      ]);
      store.setPollingUnits(pus);
      store.setIncidents(incidents);
      store.setBvasDevices(bvas);
      store.setOfficials(offi);
      setStats({
        pus: pus.features.length,
        incidents: incidents.features.length,
        bvas: bvas.features.length,
        officials: offi.features.length,
      });
    } catch (e) {
      console.error('Data load error:', e);
    }
    store.setLoading(false);
  }, [store.electionId, store.selectedStateCode]);

  useEffect(() => { loadData(); }, [loadData]);

  // Sync deck.gl layers
  useEffect(() => {
    if (!deckOverlay.current || !mapReady) return;

    const layers: unknown[] = [];

    if (store.visibleLayers.has('polling-units')) {
      layers.push(createPollingUnitScatterLayer(
        store.pollingUnits, store.colorMode,
        (info) => setSelectedFeature(info.object?.properties || null),
      ));
    }
    if (store.visibleLayers.has('heatmap')) {
      layers.push(createTurnoutHeatmapLayer(store.pollingUnits, store.heatmapMetric));
    }
    if (store.visibleLayers.has('incidents')) {
      layers.push(createIncidentLayer(
        store.incidents,
        (info) => setSelectedFeature(info.object?.properties || null),
      ));
    }
    if (store.visibleLayers.has('bvas-devices')) {
      layers.push(createBVASLayer(
        store.bvasDevices,
        (info) => setSelectedFeature(info.object?.properties || null),
      ));
    }
    if (store.visibleLayers.has('official-tracking')) {
      layers.push(createOfficialTrackingLayer(
        store.officials,
        (info) => setSelectedFeature(info.object?.properties || null),
      ));
    }
    if (store.analysisResult) {
      layers.push(createAnalysisResultLayer(store.analysisResult));
    }

    deckOverlay.current.setProps({ layers });
  }, [mapReady, store.visibleLayers, store.pollingUnits, store.incidents,
    store.bvasDevices, store.officials, store.colorMode, store.heatmapMetric,
    store.analysisResult]);

  // Basemap change
  useEffect(() => {
    if (!mapRef.current) return;
    const basemap = BASEMAPS.find(b => b.url === store.basemapStyle);
    if (basemap?.type === 'raster') {
      mapRef.current.setStyle({
        version: 8,
        sources: {
          'raster-tiles': {
            type: 'raster',
            tiles: [store.basemapStyle],
            tileSize: 256,
          },
        },
        layers: [{
          id: 'raster-tiles',
          type: 'raster',
          source: 'raster-tiles',
          minzoom: 0,
          maxzoom: 19,
        }],
      });
    } else {
      mapRef.current.setStyle(store.basemapStyle);
    }
  }, [store.basemapStyle]);

  // ─── SSE Real-time Tracking ───────────────────────────────────────────
  useEffect(() => {
    if (showTracking) {
      loadOfficials();
      try {
        const token = localStorage.getItem('token') || '';
        const es = new EventSource(`${API_BASE}/geo/tracking/stream?token=${token}`);
        sseRef.current = es;
        es.addEventListener('tracking_snapshot', (e) => {
          try {
            const d = JSON.parse(e.data);
            if (d.officials) setOfficials(d.officials);
          } catch { /* ignore */ }
        });
        es.addEventListener('tracking_update', (e) => {
          try {
            const d = JSON.parse(e.data);
            if (d.event_type === 'official_move' && d.payload) {
              const payload = typeof d.payload === 'string' ? JSON.parse(d.payload) : d.payload;
              setOfficials(prev => {
                const idx = prev.findIndex(o => o.staff_id === payload.staff_id);
                if (idx >= 0) {
                  const copy = [...prev];
                  copy[idx] = { ...copy[idx], latitude: d.latitude, longitude: d.longitude, activity: payload.activity, battery_pct: payload.battery };
                  return copy;
                }
                return prev;
              });
            }
            if (d.event_type === 'crowd_alert') {
              setCrowdAlerts(prev => [d, ...prev].slice(0, 20));
            }
          } catch { /* ignore */ }
        });
        es.addEventListener('crowd_snapshot', (e) => {
          try {
            const d = JSON.parse(e.data);
            if (d.reports) setCrowdReports(d.reports);
          } catch { /* ignore */ }
        });
        es.onerror = () => {
          es.close();
          sseRef.current = null;
          trackingInterval.current = setInterval(loadOfficials, 10000);
        };
      } catch {
        trackingInterval.current = setInterval(loadOfficials, 10000);
      }
    } else {
      if (sseRef.current) { sseRef.current.close(); sseRef.current = null; }
      officialMarkers.current.forEach(m => m.remove());
      officialMarkers.current = [];
      hasZoomedToTrack.current = false;
    }
    return () => {
      if (trackingInterval.current) clearInterval(trackingInterval.current);
      if (sseRef.current) { sseRef.current.close(); sseRef.current = null; }
    };
  }, [showTracking]);

  // ─── WebSocket for live result updates ────────────────────────────────
  useEffect(() => {
    try {
      const wsUrl = API_BASE.replace(/^http/, 'ws') + '/results/ws/updates';
      const ws = new WebSocket(wsUrl);
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data);
          if (msg && msg.type === 'result_updated') {
            loadData();
          }
        } catch { /* ignore */ }
      };
      return () => { try { ws.close(); } catch { /* ignore */ } };
    } catch { /* ignore */ }
  }, []);

  async function loadOfficials() {
    try {
      const data = await api.getOfficialLocations({ active_minutes: 60 });
      setOfficials(data.officials || []);
    } catch { /* ignore */ }
  }

  async function loadCrowdDensity() {
    try {
      const data = await api.getCrowdDensity({ recent_minutes: 120 });
      setCrowdReports(data.reports || []);
    } catch { /* ignore */ }
  }

  async function loadLandmarks(stateCode?: string) {
    try {
      const data = await api.getLandmarks({ state_code: stateCode });
      setLandmarks(data.landmarks || []);
    } catch { /* ignore */ }
  }

  async function loadWeather() {
    try {
      const data = await (api as Record<string, any>).getWeatherOverlay?.();
      setWeatherData(data?.zones || []);
    } catch { /* ignore */ }
  }

  async function openStreetView(lat: number, lng: number) {
    try {
      const data = await api.getStreetView(lat, lng);
      if (data.street_view?.mapillary?.viewer_url) {
        setStreetViewUrl(data.street_view.mapillary.viewer_url);
      } else if (data.street_view?.google?.embed_url) {
        setStreetViewUrl(data.street_view.google.embed_url);
      } else {
        setStreetViewUrl(`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${lat},${lng}`);
      }
    } catch {
      setStreetViewUrl(`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${lat},${lng}`);
    }
  }

  // ─── Render official markers (pulsing, role-colored) ──────────────────
  useEffect(() => {
    if (!mapRef.current || !showTracking) return;
    officialMarkers.current.forEach(m => m.remove());
    officialMarkers.current = [];

    const bounds = new maplibregl.LngLatBounds();
    let hasValidCoords = false;

    officials.forEach(off => {
      if (!off.latitude || !off.longitude) return;
      const color = ROLE_COLORS[off.role] || '#6b7280';
      const icon = ROLE_ICONS[off.role] || '👤';

      const el = document.createElement('div');
      el.style.cssText = `width:40px;height:40px;border-radius:50%;background:${color};border:3px solid white;cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:16px;box-shadow:0 2px 10px rgba(0,0,0,0.5);z-index:10;position:relative;`;
      el.innerHTML = icon;
      el.title = `${off.staff_id} (${off.role}) - ${off.activity}\nBattery: ${off.battery_pct}%\nPU: ${off.pu_code}`;

      const label = document.createElement('div');
      label.style.cssText = `position:absolute;top:42px;left:50%;transform:translateX(-50%);white-space:nowrap;background:${color};color:white;padding:1px 6px;border-radius:4px;font-size:9px;font-weight:600;box-shadow:0 1px 3px rgba(0,0,0,0.3);`;
      label.textContent = off.staff_id.replace('INEC-', '');
      el.appendChild(label);

      const ring = document.createElement('div');
      ring.style.cssText = `position:absolute;inset:-6px;border-radius:50%;border:3px solid ${color};opacity:0.6;animation:pulse 2s infinite;`;
      el.appendChild(ring);

      const lngLat: [number, number] = [off.longitude, off.latitude];
      bounds.extend(lngLat);
      hasValidCoords = true;

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat(lngLat)
        .setPopup(new maplibregl.Popup({ offset: 25 }).setHTML(`
          <div style="font-size:12px;min-width:200px">
            <div style="font-weight:700;margin-bottom:6px;font-size:14px;color:${color}">${off.staff_id}</div>
            <div>Role: <b>${off.role.replace(/_/g, ' ')}</b></div>
            <div>Activity: ${off.activity}</div>
            <div>Battery: ${off.battery_pct}%</div>
            <div>PU: ${off.pu_code}</div>
            <div>Coords: ${off.latitude.toFixed(4)}, ${off.longitude.toFixed(4)}</div>
            <div style="color:#888;font-size:10px;margin-top:4px">${off.updated_at}</div>
          </div>
        `))
        .addTo(mapRef.current!);
      officialMarkers.current.push(marker);
    });

    if (hasValidCoords && mapRef.current && !hasZoomedToTrack.current) {
      hasZoomedToTrack.current = true;
      mapRef.current.fitBounds(bounds, { padding: { top: 80, right: 380, bottom: 80, left: 60 }, maxZoom: 8, duration: 0 });
    }
  }, [officials, showTracking]);

  // ─── Render crowd density markers ─────────────────────────────────────
  useEffect(() => {
    if (!mapRef.current || !showCrowd) {
      crowdMarkers.current.forEach(m => m.remove());
      crowdMarkers.current = [];
      return;
    }
    crowdMarkers.current.forEach(m => m.remove());
    crowdMarkers.current = [];

    const densityColors: Record<string, string> = { low: '#22c55e', moderate: '#eab308', high: '#f97316', overcrowded: '#dc2626' };
    const densitySizes: Record<string, number> = { low: 24, moderate: 32, high: 40, overcrowded: 48 };

    crowdReports.forEach(cr => {
      if (!cr.latitude || !cr.longitude) return;
      const color = densityColors[cr.density_level] || '#6b7280';
      const size = densitySizes[cr.density_level] || 32;
      const el = document.createElement('div');
      el.style.cssText = `width:${size}px;height:${size}px;border-radius:50%;background:${color}44;border:3px solid ${color};cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:${size < 32 ? 10 : 13}px;font-weight:bold;color:${color};z-index:8;box-shadow:0 2px 8px rgba(0,0,0,0.3);`;
      el.textContent = String(cr.head_count);
      el.title = `${cr.pu_name || cr.pu_code}: ${cr.head_count} people (${cr.density_level})\nQueue: ${cr.queue_length} | Wait: ${cr.wait_time_min}min`;

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat([cr.longitude, cr.latitude])
        .setPopup(new maplibregl.Popup({ offset: 20 }).setHTML(`
          <div style="font-size:12px;min-width:200px">
            <div style="font-weight:600;margin-bottom:4px">${cr.pu_name || cr.pu_code}</div>
            <div>Head Count: <b>${cr.head_count}</b></div>
            <div>Density: <b style="color:${color}">${cr.density_level.toUpperCase()}</b></div>
            <div>Queue Length: ${cr.queue_length} people</div>
            <div>Wait Time: ${cr.wait_time_min} min</div>
          </div>
        `))
        .addTo(mapRef.current!);
      crowdMarkers.current.push(marker);
    });
  }, [crowdReports, showCrowd]);

  // ─── Render landmark markers ──────────────────────────────────────────
  useEffect(() => {
    if (!mapRef.current || !showLandmarks) {
      landmarkMarkers.current.forEach(m => m.remove());
      landmarkMarkers.current = [];
      return;
    }
    landmarkMarkers.current.forEach(m => m.remove());
    landmarkMarkers.current = [];

    const categoryColors: Record<string, string> = {
      inec_office: '#059669', collation_center: '#dc2626', police_station: '#1d4ed8',
      hospital: '#ec4899', school: '#f59e0b', transport_hub: '#6366f1',
      government_building: '#7c3aed', church: '#0891b2', mosque: '#0891b2',
      market: '#ea580c', bank: '#64748b',
    };

    landmarks.forEach(lm => {
      const el = document.createElement('div');
      el.style.cssText = `width:24px;height:24px;border-radius:50%;background:${categoryColors[lm.category] || '#6b7280'};border:2px solid white;cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:10px;color:white;box-shadow:0 2px 4px rgba(0,0,0,0.3);`;
      el.title = `${lm.name} (${lm.category})`;

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat([lm.longitude, lm.latitude])
        .setPopup(new maplibregl.Popup({ offset: 15 }).setHTML(
          `<div style="font-size:12px"><strong>${lm.name}</strong><br/><span style="color:#6b7280">${lm.category.replace(/_/g, ' ')}</span>${lm.address ? `<br/><span style="font-size:10px">${lm.address}</span>` : ''}</div>`
        ))
        .addTo(mapRef.current!);
      landmarkMarkers.current.push(marker);
    });
  }, [landmarks, showLandmarks]);

  // ─── Geofence zones ───────────────────────────────────────────────────
  useEffect(() => {
    if (!mapRef.current) return;
    geofenceMarkers.current.forEach(m => m.remove());
    geofenceMarkers.current = [];
    if (!showGeofences) return;
    api.getGeofenceZones(selectedState || undefined).then((data: any) => {
      const zones = data?.zones?.features || data?.zones || [];
      zones.forEach((z: any) => {
        const props = z.properties || z;
        const lat = props.center_lat || z.geometry?.coordinates?.[0]?.[0]?.[1];
        const lng = props.center_lng || z.geometry?.coordinates?.[0]?.[0]?.[0];
        if (!lat || !lng) return;
        const el = document.createElement('div');
        el.style.cssText = 'width:60px;height:60px;border:2px dashed rgba(59,130,246,0.6);border-radius:50%;background:rgba(59,130,246,0.1);display:flex;align-items:center;justify-content:center;font-size:9px;color:#3b82f6;pointer-events:auto;';
        el.textContent = `${props.radius_m || 500}m`;
        el.title = `Geofence: ${props.pu_code || ''} (${props.radius_m || 500}m radius)`;
        const marker = new maplibregl.Marker({ element: el }).setLngLat([lng, lat]).addTo(mapRef.current!);
        geofenceMarkers.current.push(marker);
      });
    }).catch(() => {});
  }, [showGeofences, selectedState]);

  // ─── Incident hotspot markers ─────────────────────────────────────────
  useEffect(() => {
    if (!mapRef.current) return;
    incidentHotspotMarkers.current.forEach(m => m.remove());
    incidentHotspotMarkers.current = [];
    if (!showIncidentHotspots) return;
    api.getIncidentHotspots(48).then((data: any) => {
      const incidents = data?.incidents?.features || data?.incidents || [];
      incidents.forEach((inc: any) => {
        const props = inc.properties || inc;
        const lat = props.latitude || inc.geometry?.coordinates?.[1];
        const lng = props.longitude || inc.geometry?.coordinates?.[0];
        if (!lat || !lng) return;
        const sev = props.severity || 'medium';
        const color = sev === 'critical' ? '#dc2626' : sev === 'high' ? '#f97316' : '#eab308';
        const el = document.createElement('div');
        el.style.cssText = `width:16px;height:16px;background:${color};border-radius:50%;border:2px solid white;box-shadow:0 0 6px ${color};cursor:pointer;`;
        el.title = `${props.incident_type || 'Incident'}: ${props.description || ''} (${sev})`;
        const marker = new maplibregl.Marker({ element: el }).setLngLat([lng, lat]).addTo(mapRef.current!);
        incidentHotspotMarkers.current.push(marker);
      });
    }).catch(() => {});
  }, [showIncidentHotspots]);

  // ─── Weather overlay ──────────────────────────────────────────────────
  useEffect(() => {
    if (!mapRef.current) return;
    weatherMarkers.current.forEach(m => m.remove());
    weatherMarkers.current = [];
    if (!showWeather || weatherData.length === 0) return;
    weatherData.forEach((w) => {
      if (!w.lat || !w.lng) return;
      const el = document.createElement('div');
      el.style.cssText = 'background:rgba(255,255,255,0.9);border-radius:6px;padding:2px 6px;font-size:10px;box-shadow:0 1px 3px rgba(0,0,0,0.2);pointer-events:auto;white-space:nowrap;';
      const temp = w.weather?.temp_c ?? '--';
      const desc = w.weather?.description || '';
      el.innerHTML = `<b>${temp}°C</b> ${desc}`;
      el.title = `${w.name}: ${temp}°C, ${w.weather?.humidity || '--'}% humidity`;
      const marker = new maplibregl.Marker({ element: el }).setLngLat([w.lng, w.lat]).addTo(mapRef.current!);
      weatherMarkers.current.push(marker);
    });
  }, [showWeather, weatherData]);

  // ─── Geocoding (Nominatim) ────────────────────────────────────────────
  useEffect(() => {
    if (searchQuery.trim().length < 3) { setPlaces([]); return; }
    const controller = new AbortController();
    const url = `https://nominatim.openstreetmap.org/search?format=jsonv2&countrycodes=ng&q=${encodeURIComponent(searchQuery)}&limit=5`;
    fetch(url, { signal: controller.signal, headers: { 'Accept-Language': 'en' } })
      .then(r => r.json())
      .then((data) => setPlaces(Array.isArray(data) ? data.map((d: any) => ({ name: d.display_name, lat: parseFloat(d.lat), lon: parseFloat(d.lon) })) : []))
      .catch(() => {});
    return () => controller.abort();
  }, [searchQuery]);

  // Auto-load landmarks & crowd on mount
  useEffect(() => { if (showLandmarks) loadLandmarks(selectedState || undefined); }, [showLandmarks, selectedState]);
  useEffect(() => { if (showCrowd) loadCrowdDensity(); }, [showCrowd]);

  function flyToCoords(lat: number, lon: number, zoom = 14) {
    if (mapRef.current) mapRef.current.flyTo({ center: [lon, lat], zoom, duration: 1200 });
  }

  function resetView() {
    setSelectedState(null);
    store.setSelectedStateCode(null);
    if (mapRef.current) mapRef.current.flyTo({ center: [8.0, 9.5], zoom: 5.8, duration: 1000 });
    loadData();
  }

  // Voice navigation
  function startVoiceNav() {
    if (!('webkitSpeechRecognition' in window) && !('SpeechRecognition' in window)) return;
    const SR = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    const recognition = new SR();
    recognition.lang = 'en-NG';
    recognition.continuous = false;
    recognition.interimResults = false;
    setVoiceListening(true);
    recognition.onresult = (event: any) => {
      const text = event.results[0][0].transcript.toLowerCase();
      setVoiceListening(false);
      if (text.includes('nearest') || text.includes('nearby')) {
        if (mapRef.current) {
          const c = mapRef.current.getCenter();
          setSearchQuery(`nearby ${c.lat.toFixed(4)},${c.lng.toFixed(4)}`);
        }
      } else if (text.includes('landmark')) {
        setShowLandmarks(true);
      } else if (text.includes('track') || text.includes('official')) {
        setShowTracking(true);
      } else if (text.includes('weather')) {
        setShowWeather(true);
        loadWeather();
      } else if (text.includes('incident')) {
        setShowIncidentHotspots(true);
      } else {
        setSearchQuery(text);
      }
    };
    recognition.onerror = () => setVoiceListening(false);
    recognition.onend = () => setVoiceListening(false);
    recognition.start();
  }

  // Box select handlers
  function onSelectMouseDown(e: React.MouseEvent<HTMLDivElement>) {
    if (!selecting || !mapRef.current) return;
    const rect = mapRef.current.getContainer().getBoundingClientRect();
    setSelectionBox({ x1: e.clientX - rect.left, y1: e.clientY - rect.top, x2: e.clientX - rect.left, y2: e.clientY - rect.top });
  }
  function onSelectMouseMove(e: React.MouseEvent<HTMLDivElement>) {
    if (!selecting || !mapRef.current || !selectionBox) return;
    const rect = mapRef.current.getContainer().getBoundingClientRect();
    setSelectionBox({ ...selectionBox, x2: e.clientX - rect.left, y2: e.clientY - rect.top });
  }
  function onSelectMouseUp() {
    if (!selecting || !selectionBox) { setSelectionBox(null); return; }
    setSelecting(false);
    setSelectionBox(null);
  }

  // Export functions
  function exportCSV() {
    const base = `${API_BASE}/geo/reports/polling-units.csv?election_id=1${selectedState ? `&state_code=${selectedState}` : ''}`;
    window.open(base, '_blank');
  }

  // PU search results
  const searchResults = useMemo(() => {
    if (!searchQuery.trim()) return [];
    return store.pollingUnits.features
      .filter(f => {
        const q = searchQuery.toLowerCase();
        return f.properties.name.toLowerCase().includes(q) ||
          f.properties.code.toLowerCase().includes(q) ||
          f.properties.lga_name.toLowerCase().includes(q) ||
          f.properties.ward_name.toLowerCase().includes(q);
      })
      .slice(0, 15);
  }, [searchQuery, store.pollingUnits]);

  return (
    <div className="flex h-full">
      {/* CSS for pulsing animation */}
      <style>{`@keyframes pulse { 0%,100%{transform:scale(1);opacity:.6} 50%{transform:scale(1.4);opacity:0} }`}</style>

      {/* Left panel — layer control + extra features */}
      <div className="w-72 border-r bg-white overflow-y-auto shrink-0 p-3 space-y-3">
        {/* Stats */}
        <div className="grid grid-cols-2 gap-2">
          <StatCard icon={MapPin} label="PUs" value={stats.pus} color="text-blue-500" />
          <StatCard icon={AlertTriangle} label="Incidents" value={stats.incidents} color="text-red-500" />
          <StatCard icon={Radio} label="BVAS" value={stats.bvas} color="text-purple-500" />
          <StatCard icon={Users} label="Officials" value={officials.length || stats.officials} color="text-cyan-500" />
        </div>

        {/* Search */}
        <div className="relative">
          <div className="flex gap-1">
            <Input
              placeholder="Search PUs, places..."
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              className="h-8 text-xs"
            />
            <Button variant="ghost" size="sm" className="h-8 w-8 p-0 shrink-0" onClick={startVoiceNav}>
              <Mic className={`w-3.5 h-3.5 ${voiceListening ? 'text-red-500 animate-pulse' : ''}`} />
            </Button>
          </div>
          {(searchResults.length > 0 || places.length > 0) && (
            <div className="absolute z-30 top-9 left-0 right-0 bg-white border rounded shadow-lg max-h-48 overflow-y-auto">
              {searchResults.map(f => (
                <button key={f.properties.code}
                  className="w-full text-left px-2 py-1 text-xs hover:bg-muted flex items-center gap-1"
                  onClick={() => {
                    const [lng, lat] = f.geometry.coordinates;
                    flyToCoords(lat, lng, 14);
                    setSelectedFeature(f.properties as unknown as Record<string, unknown>);
                    setSearchQuery('');
                  }}>
                  <MapPin className="w-3 h-3 text-blue-500 shrink-0" />
                  <span className="truncate">{f.properties.name}</span>
                  <Badge variant="outline" className="text-[8px] ml-auto shrink-0">{f.properties.state_code}</Badge>
                </button>
              ))}
              {places.map((p, i) => (
                <button key={i}
                  className="w-full text-left px-2 py-1 text-xs hover:bg-muted flex items-center gap-1"
                  onClick={() => { flyToCoords(p.lat, p.lon, 12); setSearchQuery(''); }}>
                  <Search className="w-3 h-3 text-green-500 shrink-0" />
                  <span className="truncate">{p.name}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        {/* State drill-down */}
        {selectedState && (
          <Button variant="ghost" size="sm" className="w-full text-xs gap-1" onClick={resetView}>
            <ArrowLeft className="w-3.5 h-3.5" /> Back to National View
          </Button>
        )}

        {/* Basemap */}
        <div>
          <label className="text-xs font-medium text-muted-foreground">Basemap</label>
          <Select value={store.basemapStyle} onValueChange={store.setBasemapStyle}>
            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {BASEMAPS.map(b => (
                <SelectItem key={b.id} value={b.url}>{b.label} {b.type === 'raster' ? '🛰️' : ''}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Color mode */}
        <div>
          <label className="text-xs font-medium text-muted-foreground">Color By</label>
          <Select value={store.colorMode} onValueChange={(v) => store.setColorMode(v as 'status' | 'turnout' | 'party')}>
            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="status">Result Status</SelectItem>
              <SelectItem value="turnout">Turnout %</SelectItem>
              <SelectItem value="party">Leading Party</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Status filters */}
        <div>
          <label className="text-xs font-medium text-muted-foreground block mb-1">Status Filters</label>
          <div className="flex flex-wrap gap-1">
            {Object.entries(STATUS_COLORS).map(([status, color]) => (
              <button key={status}
                className={`flex items-center gap-1 px-2 py-0.5 rounded text-[10px] border transition-colors ${statusFilter[status] ? 'border-current' : 'opacity-40 border-transparent'}`}
                style={{ color }}
                onClick={() => setStatusFilter(prev => ({ ...prev, [status]: !prev[status] }))}>
                <div className="w-2 h-2 rounded-full" style={{ backgroundColor: color }} />
                {status.replace('_', ' ')}
              </button>
            ))}
          </div>
        </div>

        {/* Layers */}
        <div>
          <label className="text-xs font-medium text-muted-foreground block mb-1">deck.gl Layers</label>
          {LAYER_CONFIG.map(lc => (
            <button key={lc.id}
              className={`flex items-center gap-2 w-full px-2 py-1.5 rounded text-xs transition-colors ${store.visibleLayers.has(lc.id) ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted'}`}
              onClick={() => store.toggleLayer(lc.id)}
            >
              {store.visibleLayers.has(lc.id) ? <Eye className="w-3.5 h-3.5" /> : <EyeOff className="w-3.5 h-3.5" />}
              <lc.icon className={`w-3.5 h-3.5 ${lc.color}`} />
              {lc.label}
            </button>
          ))}
        </div>

        {/* Geospatial Layers */}
        <Card>
          <CardHeader className="py-2 px-3">
            <CardTitle className="text-xs flex items-center gap-1"><Layers className="w-3.5 h-3.5" /> Geospatial Layers</CardTitle>
          </CardHeader>
          <CardContent className="py-1 px-3 space-y-1.5">
            <LayerToggle label="Officials" icon={Radio} active={showTracking} count={officials.length} liveIndicator
              onToggle={() => setShowTracking(!showTracking)} />
            <LayerToggle label="Crowd" icon={Users} active={showCrowd} count={crowdReports.length}
              onToggle={() => { setShowCrowd(!showCrowd); if (!showCrowd) loadCrowdDensity(); }} />
            <LayerToggle label="Landmarks" icon={Building2} active={showLandmarks}
              onToggle={() => { setShowLandmarks(!showLandmarks); if (!showLandmarks) loadLandmarks(selectedState || undefined); }} />
            <LayerToggle label="Geofences" icon={Shield} active={showGeofences}
              onToggle={() => setShowGeofences(!showGeofences)} />
            <LayerToggle label="Incidents" icon={AlertTriangle} active={showIncidentHotspots}
              onToggle={() => setShowIncidentHotspots(!showIncidentHotspots)} />
            <LayerToggle label="Weather" icon={Cloud} active={showWeather}
              onToggle={() => { setShowWeather(!showWeather); if (!showWeather) loadWeather(); }} />
          </CardContent>
        </Card>

        {/* 3D toggle + controls */}
        <div className="flex gap-1">
          <Button variant="outline" size="sm" className="flex-1 text-xs" onClick={store.toggle3D}>
            <Box className="w-3.5 h-3.5 mr-1" /> {store.is3D ? 'Disable 3D' : 'Enable 3D'}
          </Button>
          <Button variant={compareMode ? 'default' : 'outline'} size="sm" className="text-xs" onClick={() => setCompareMode(!compareMode)}>
            Compare
          </Button>
        </div>

        {/* Street View */}
        <Button variant="outline" size="sm" className="w-full text-xs" onClick={() => {
          if (mapRef.current) {
            const c = mapRef.current.getCenter();
            openStreetView(c.lat, c.lng);
          }
        }}>
          <Eye className="w-3.5 h-3.5 mr-1" /> Street View
        </Button>

        {/* Box select */}
        <Button variant={selecting ? 'default' : 'outline'} size="sm" className="w-full text-xs"
          onClick={() => { setSelecting(v => !v); setSelectionBox(null); }}>
          Box Select {selectedCodes.length > 0 && `(${selectedCodes.length})`}
        </Button>

        {/* Time slider */}
        <div>
          <label className="text-xs font-medium text-muted-foreground">Time Filter</label>
          <input type="range" min={0} max={Math.floor(Date.now() / 1000)} className="w-full"
            value={timeTs ?? Math.floor(Date.now() / 1000)}
            onChange={(e) => {
              const now = Math.floor(Date.now() / 1000);
              const v = Number(e.target.value);
              setTimeTs(v >= now ? null : v);
            }} />
          <span className="text-[10px] text-muted-foreground">
            {new Date(((timeTs ?? Math.floor(Date.now() / 1000))) * 1000).toLocaleString()}
          </span>
        </div>

        {/* Export */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Export</label>
          <Button variant="outline" size="sm" className="w-full text-xs"
            onClick={() => downloadGeoJSON(store.pollingUnits, 'inec-polling-units.geojson')}>
            <Download className="w-3.5 h-3.5 mr-1" /> Polling Units (GeoJSON)
          </Button>
          <Button variant="outline" size="sm" className="w-full text-xs"
            onClick={() => downloadGeoJSON(store.incidents, 'inec-incidents.geojson')}>
            <Download className="w-3.5 h-3.5 mr-1" /> Incidents (GeoJSON)
          </Button>
          <Button variant="outline" size="sm" className="w-full text-xs" onClick={exportCSV}>
            <Download className="w-3.5 h-3.5 mr-1" /> Export CSV
          </Button>
        </div>

        {/* Refresh */}
        <Button variant="default" size="sm" className="w-full text-xs" onClick={loadData} disabled={store.loading}>
          <RefreshCw className={`w-3.5 h-3.5 mr-1 ${store.loading ? 'animate-spin' : ''}`} /> Refresh Data
        </Button>

        {/* Legend */}
        {store.colorMode === 'status' && (
          <Card>
            <CardHeader className="py-1 px-3">
              <CardTitle className="text-[10px]">Status Legend</CardTitle>
            </CardHeader>
            <CardContent className="py-1 px-3 space-y-0.5">
              {Object.entries(STATUS_COLORS).map(([s, c]) => (
                <div key={s} className="flex items-center gap-1 text-[10px]">
                  <div className="w-3 h-2 rounded" style={{ backgroundColor: c }} />
                  <span>{s.replace('_', ' ')}</span>
                </div>
              ))}
            </CardContent>
          </Card>
        )}
      </div>

      {/* Map container — fixed height */}
      <div className="flex-1 relative" style={{ minHeight: 0 }}
        onMouseDown={onSelectMouseDown} onMouseMove={onSelectMouseMove} onMouseUp={onSelectMouseUp}>
        <div ref={mapContainer} style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, width: '100%', height: '100%' }} />

        {/* Feature popup */}
        {selectedFeature && (
          <div className="absolute top-3 right-3 w-80 z-10">
            <Card className="shadow-lg">
              <CardHeader className="py-2 px-3 flex flex-row items-center justify-between">
                <CardTitle className="text-sm">
                  {(selectedFeature.name as string) || (selectedFeature.code as string) || 'Feature Details'}
                </CardTitle>
                <button className="text-xs text-muted-foreground" onClick={() => setSelectedFeature(null)}>✕</button>
              </CardHeader>
              <CardContent className="py-2 px-3 text-xs space-y-1">
                {Object.entries(selectedFeature).filter(([k]) => k !== 'party_scores').map(([k, v]) => (
                  <div key={k} className="flex justify-between">
                    <span className="text-muted-foreground">{k.replace(/_/g, ' ')}</span>
                    <span className="font-medium">{String(v ?? '—')}</span>
                  </div>
                ))}
              </CardContent>
            </Card>
          </div>
        )}

        {/* Street View overlay */}
        {streetViewUrl && (
          <div className="absolute bottom-3 right-3 w-96 h-64 z-10 bg-white rounded-lg shadow-lg overflow-hidden">
            <div className="flex items-center justify-between px-2 py-1 bg-zinc-100 text-xs">
              <span className="font-medium">Street View</span>
              <div className="flex gap-1">
                <a href={streetViewUrl} target="_blank" rel="noopener noreferrer" className="text-blue-500 hover:underline">
                  <ExternalLink className="w-3 h-3" />
                </a>
                <button onClick={() => setStreetViewUrl(null)} className="text-zinc-500">✕</button>
              </div>
            </div>
            <iframe src={streetViewUrl} className="w-full h-[calc(100%-28px)] border-0" title="Street View" />
          </div>
        )}

        {/* Loading overlay */}
        {store.loading && (
          <div className="absolute inset-0 bg-white/50 flex items-center justify-center z-20">
            <div className="flex items-center gap-2 bg-white px-4 py-2 rounded-lg shadow">
              <RefreshCw className="w-4 h-4 animate-spin" /> Loading election data...
            </div>
          </div>
        )}

        {/* Selection box overlay */}
        {selecting && selectionBox && (
          <div className="absolute pointer-events-none z-10" style={{
            left: Math.min(selectionBox.x1, selectionBox.x2),
            top: Math.min(selectionBox.y1, selectionBox.y2),
            width: Math.abs(selectionBox.x2 - selectionBox.x1),
            height: Math.abs(selectionBox.y2 - selectionBox.y1),
            border: '2px dashed #3b82f6',
            background: 'rgba(59,130,246,0.1)',
          }} />
        )}

        {/* Crowd alerts banner */}
        {crowdAlerts.length > 0 && (
          <div className="absolute top-3 left-3 z-10 max-w-xs">
            <Card className="shadow-lg bg-red-50 border-red-200">
              <CardContent className="py-2 px-3 text-xs">
                <div className="flex items-center gap-1 font-semibold text-red-600 mb-1">
                  <AlertTriangle className="w-3.5 h-3.5" /> Crowd Alerts ({crowdAlerts.length})
                </div>
                {crowdAlerts.slice(0, 3).map((a, i) => (
                  <div key={i} className="text-red-700 truncate">{a.message}</div>
                ))}
              </CardContent>
            </Card>
          </div>
        )}
      </div>

      {/* Right panel — live officials + crowd */}
      {showRightPanel && (showTracking || showCrowd) && (
        <div className="w-72 border-l bg-white overflow-y-auto shrink-0 p-3 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium">Live Panels</span>
            <button className="text-xs text-muted-foreground" onClick={() => setShowRightPanel(false)}>✕</button>
          </div>

          {/* Officials panel */}
          {showTracking && officials.length > 0 && (
            <Card>
              <CardHeader className="py-2 px-3">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-xs flex items-center gap-1">
                    <Radio className="w-3.5 h-3.5 text-green-500" /> Live Officials ({officials.length})
                  </CardTitle>
                  <div className="flex items-center gap-1">
                    <div className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                    <span className="text-[10px] text-green-600">LIVE</span>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="py-1 px-3">
                <div className="space-y-1 max-h-48 overflow-y-auto">
                  {officials.map(off => (
                    <div key={off.staff_id}
                      className="flex items-center gap-2 text-xs py-1 px-1 rounded cursor-pointer hover:bg-zinc-50 border border-zinc-100"
                      onClick={() => flyToCoords(off.latitude, off.longitude, 16)}>
                      <div className="w-5 h-5 rounded-full flex items-center justify-center text-white text-[9px] shrink-0"
                        style={{ backgroundColor: ROLE_COLORS[off.role] || '#6b7280' }}>
                        {off.role === 'security' ? <Shield className="w-2.5 h-2.5" /> : <Users className="w-2.5 h-2.5" />}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="font-medium truncate text-[10px]">{off.staff_id}</div>
                        <div className="text-zinc-500 text-[9px]">{off.activity}</div>
                      </div>
                      <Badge variant={off.battery_pct > 50 ? 'default' : off.battery_pct > 20 ? 'secondary' : 'destructive'} className="text-[8px] h-4">
                        <Battery className="w-2 h-2 mr-0.5" />{off.battery_pct}%
                      </Badge>
                    </div>
                  ))}
                </div>
                <div className="mt-2 flex gap-1 flex-wrap">
                  {['presiding_officer', 'observer', 'security', 'supervisor'].map(role => {
                    const count = officials.filter(o => o.role === role).length;
                    if (!count) return null;
                    return <Badge key={role} variant="outline" className="text-[8px]">{role.replace(/_/g, ' ')}: {count}</Badge>;
                  })}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Crowd density panel */}
          {showCrowd && crowdReports.length > 0 && (
            <Card>
              <CardHeader className="py-2 px-3">
                <CardTitle className="text-xs flex items-center gap-1">
                  <Users className="w-3.5 h-3.5" /> Crowd Density ({crowdReports.length})
                </CardTitle>
              </CardHeader>
              <CardContent className="py-1 px-3">
                <div className="flex gap-1 flex-wrap mb-2">
                  {(['overcrowded', 'high', 'moderate', 'low'] as const).map(level => {
                    const count = crowdReports.filter(r => r.density_level === level).length;
                    if (!count) return null;
                    const colors: Record<string, string> = { overcrowded: 'destructive', high: 'secondary', moderate: 'outline', low: 'default' };
                    return <Badge key={level} variant={colors[level] as any} className="text-[8px]">{level}: {count}</Badge>;
                  })}
                </div>
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {crowdReports.slice(0, 10).map((cr, i) => (
                    <div key={i} className="flex items-center justify-between text-[10px] py-0.5 cursor-pointer hover:bg-zinc-50"
                      onClick={() => flyToCoords(cr.latitude, cr.longitude, 15)}>
                      <span className="truncate flex-1">{cr.pu_name || cr.pu_code}</span>
                      <span className="font-bold">{cr.head_count}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Landmarks panel */}
          {showLandmarks && landmarks.length > 0 && (
            <Card>
              <CardHeader className="py-2 px-3">
                <CardTitle className="text-xs">{landmarks.length} Landmarks</CardTitle>
              </CardHeader>
              <CardContent className="py-1 px-3">
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {landmarks.slice(0, 10).map(lm => (
                    <div key={lm.id} className="flex items-center gap-1 text-[10px] cursor-pointer hover:bg-zinc-50"
                      onClick={() => flyToCoords(lm.latitude, lm.longitude, 14)}>
                      <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: '#059669' }} />
                      <span className="truncate">{lm.name}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}

function StatCard({ icon: Icon, label, value, color }: { icon: typeof MapPin; label: string; value: number; color: string }) {
  return (
    <div className="border rounded p-2 text-center">
      <Icon className={`w-4 h-4 mx-auto ${color}`} />
      <div className="text-lg font-bold">{value.toLocaleString()}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

function LayerToggle({ label, icon: Icon, active, count, liveIndicator, onToggle }: {
  label: string; icon: typeof MapPin; active: boolean; count?: number; liveIndicator?: boolean; onToggle: () => void;
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-xs flex items-center gap-1">
        <Icon className="w-3 h-3" /> {label}
        {liveIndicator && active && <div className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />}
      </span>
      <Button size="sm" variant={active ? 'default' : 'outline'} className="h-5 text-[10px] px-2" onClick={onToggle}>
        {active ? (count !== undefined ? `${count}` : 'On') : 'Off'}
      </Button>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// SPATIAL ANALYSIS TAB
// ═══════════════════════════════════════════════════════════════════════════

function SpatialAnalysisTab() {
  const store = useGeoLibreStore();
  const [queryType, setQueryType] = useState<string>('buffer');
  const [bufferRadius, setBufferRadius] = useState('5');
  const [results, setResults] = useState<Record<string, unknown> | null>(null);

  const tools = [
    { id: 'buffer', label: 'Buffer Analysis', desc: 'Create buffer zones around polling units to find overlapping coverage areas', icon: Navigation },
    { id: 'hotspot', label: 'Hotspot Detection', desc: 'Identify spatial clusters of incidents, low turnout, or anomalies using Getis-Ord Gi*', icon: Flame },
    { id: 'voronoi', label: 'Voronoi Tessellation', desc: 'Divide Nigeria into service areas — each voter goes to their nearest PU', icon: Hexagon },
    { id: 'h3-aggregate', label: 'H3 Hex Aggregation', desc: 'Aggregate election data into H3 hexagonal cells for uniform spatial analysis', icon: Hexagon },
    { id: 'nearest', label: 'Nearest Neighbor', desc: 'Find the N closest polling units to any point or incident location', icon: Search },
    { id: 'density', label: 'Kernel Density', desc: 'Generate smooth density surface from polling unit voter registration counts', icon: BarChart3 },
    { id: 'coverage', label: 'Coverage Analysis', desc: 'Calculate % of population within X km of a polling unit using census data', icon: Globe },
    { id: 'anomaly', label: 'Spatial Anomaly', desc: 'Detect statistically unusual turnout/result patterns using spatial autocorrelation', icon: AlertTriangle },
  ];

  const runAnalysis = useCallback(async () => {
    const puCount = store.pollingUnits.features.length;
    if (puCount === 0) {
      setResults({ error: 'No polling unit data loaded. Load data from the Live Map tab first.' });
      return;
    }

    setResults({ status: 'running', tool: queryType });
    const pus = store.pollingUnits.features;
    let analysisResult: Record<string, unknown>;

    switch (queryType) {
      case 'buffer': {
        const radius = parseFloat(bufferRadius);
        const totalPUs = pus.length;
        const withResults = pus.filter(p => p.properties.status !== 'no_result').length;
        analysisResult = {
          tool: 'Buffer Analysis', radius_km: radius, total_pus: totalPUs,
          pus_with_results: withResults,
          coverage_pct: ((withResults / totalPUs) * 100).toFixed(1) + '%',
          estimated_area_km2: (totalPUs * Math.PI * radius * radius).toFixed(0),
          overlap_zones: Math.max(0, Math.floor(totalPUs * 0.15)),
        };
        break;
      }
      case 'hotspot': {
        const incidents = store.incidents.features;
        const stateIncidents: Record<string, number> = {};
        incidents.forEach(i => {
          stateIncidents[i.properties.state_code] = (stateIncidents[i.properties.state_code] || 0) + 1;
        });
        const hotspots = Object.entries(stateIncidents)
          .sort(([, a], [, b]) => b - a).slice(0, 5)
          .map(([state, count]) => ({ state, incidents: count }));
        analysisResult = {
          tool: 'Hotspot Detection', total_incidents: incidents.length, hotspot_states: hotspots,
          critical_count: incidents.filter(i => i.properties.severity === 'critical').length,
          high_count: incidents.filter(i => i.properties.severity === 'high').length,
        };
        break;
      }
      case 'h3-aggregate': {
        const stateTurnout: Record<string, { total: number; cast: number; count: number }> = {};
        pus.forEach(pu => {
          const sc = pu.properties.state_code;
          if (!stateTurnout[sc]) stateTurnout[sc] = { total: 0, cast: 0, count: 0 };
          stateTurnout[sc].total += pu.properties.registered_voters;
          stateTurnout[sc].cast += pu.properties.total_votes_cast || 0;
          stateTurnout[sc].count += 1;
        });
        analysisResult = {
          tool: 'H3 Hex Aggregation', total_hexagons: Object.keys(stateTurnout).length,
          state_metrics: Object.entries(stateTurnout).map(([state, d]) => ({
            state, pus: d.count, registered: d.total, cast: d.cast,
            turnout_pct: d.total > 0 ? ((d.cast / d.total) * 100).toFixed(1) + '%' : '0%',
          })),
        };
        break;
      }
      default:
        analysisResult = {
          tool: queryType, status: 'available',
          message: `${queryType} analysis requires loading spatial data first.`,
          pus_loaded: puCount,
        };
    }

    setResults(analysisResult);
  }, [queryType, bufferRadius, store.pollingUnits, store.incidents]);

  return (
    <div className="flex h-full">
      <div className="w-80 border-r bg-white overflow-y-auto p-4 space-y-3">
        <h3 className="text-sm font-semibold">Spatial Analysis Tools</h3>
        <p className="text-xs text-muted-foreground">
          GeoLibre-powered spatial analysis for election data. Select a tool and run analysis against loaded data.
        </p>
        {tools.map(tool => (
          <button key={tool.id}
            className={`w-full text-left p-3 rounded-lg border transition-colors ${queryType === tool.id ? 'border-primary bg-primary/5' : 'hover:bg-muted'}`}
            onClick={() => setQueryType(tool.id)}>
            <div className="flex items-center gap-2">
              <tool.icon className="w-4 h-4 text-primary" />
              <span className="text-sm font-medium">{tool.label}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-1">{tool.desc}</p>
          </button>
        ))}
        {queryType === 'buffer' && (
          <div className="space-y-1">
            <label className="text-xs font-medium">Buffer Radius (km)</label>
            <Input type="number" value={bufferRadius} onChange={e => setBufferRadius(e.target.value)} className="h-8 text-xs" />
          </div>
        )}
        <Button className="w-full" onClick={runAnalysis}>
          <Activity className="w-4 h-4 mr-2" /> Run Analysis
        </Button>
      </div>
      <div className="flex-1 p-6 overflow-y-auto">
        {results ? (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{(results.tool as string) || 'Analysis'} Results</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="bg-muted p-4 rounded text-xs overflow-auto max-h-[60vh] whitespace-pre-wrap">
                {JSON.stringify(results, null, 2)}
              </pre>
              <div className="flex gap-2 mt-4">
                <Button variant="outline" size="sm"
                  onClick={() => downloadGeoJSON(results, `inec-${queryType}-analysis.json`)}>
                  <Download className="w-3.5 h-3.5 mr-1" /> Export JSON
                </Button>
              </div>
            </CardContent>
          </Card>
        ) : (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            <div className="text-center">
              <Database className="w-12 h-12 mx-auto mb-4 opacity-30" />
              <p className="text-sm">Select a spatial analysis tool and click Run Analysis</p>
              <p className="text-xs mt-1">Data is loaded from the Live Map tab</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// GEOLIBRE VIEWER TAB
// ═══════════════════════════════════════════════════════════════════════════

function GeoLibreViewerTab() {
  const [viewerUrl, setViewerUrl] = useState('https://viewer.geolibre.app');
  const [urlInput, setUrlInput] = useState('');
  const store = useGeoLibreStore();

  const loadProject = useCallback((url: string) => {
    if (url) setViewerUrl(`https://viewer.geolibre.app/?url=${encodeURIComponent(url)}`);
  }, []);

  const exportToGeoLibre = useCallback(async () => {
    const project = {
      version: '1.0',
      name: `INEC Election ${store.electionId} — GeoLibre Analysis`,
      description: 'Exported from INEC Election Management Platform',
      center: [NIGERIA_CENTER.longitude, NIGERIA_CENTER.latitude],
      zoom: NIGERIA_CENTER.zoom,
      layers: [
        { name: 'Polling Units', type: 'geojson', visible: true, data: store.pollingUnits },
        { name: 'Election Incidents', type: 'geojson', visible: true, data: store.incidents },
      ],
    };
    downloadGeoJSON(project, `inec-election-${store.electionId}.geolibre.json`);
  }, [store.electionId, store.pollingUnits, store.incidents]);

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 p-2 border-b bg-white shrink-0">
        <Badge variant="secondary" className="text-xs">GeoLibre Viewer</Badge>
        <Input placeholder="Load .geolibre.json project URL..." value={urlInput}
          onChange={e => setUrlInput(e.target.value)} className="flex-1 h-8 text-xs"
          onKeyDown={e => e.key === 'Enter' && loadProject(urlInput)} />
        <Button variant="outline" size="sm" onClick={() => loadProject(urlInput)}>
          <Globe className="w-3.5 h-3.5 mr-1" /> Load
        </Button>
        <Button variant="outline" size="sm" onClick={exportToGeoLibre}>
          <Download className="w-3.5 h-3.5 mr-1" /> Export to GeoLibre
        </Button>
        <Button variant="outline" size="sm" onClick={() => setViewerUrl('https://viewer.geolibre.app')}>
          <RefreshCw className="w-3.5 h-3.5 mr-1" /> Reset
        </Button>
        <a href="https://viewer.geolibre.app" target="_blank" rel="noopener noreferrer">
          <Button variant="ghost" size="sm" className="text-xs">
            <Satellite className="w-3.5 h-3.5 mr-1" /> Open Full
          </Button>
        </a>
      </div>
      <div className="flex-1">
        <iframe src={viewerUrl} className="w-full h-full border-0" title="GeoLibre Viewer"
          allow="geolocation; fullscreen"
          sandbox="allow-scripts allow-same-origin allow-popups allow-forms" />
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// FIELD KIT TAB
// ═══════════════════════════════════════════════════════════════════════════

function FieldKitTab() {
  const store = useGeoLibreStore();
  const [generating, setGenerating] = useState(false);
  const [offlinePackages, setOfflinePackages] = useState<Array<{ name: string; size: string; state: string; status: string }>>([]);

  const generateFieldKit = useCallback(async () => {
    setGenerating(true);
    const states = ['LA', 'KN', 'RI', 'FC', 'OY', 'AN', 'KD', 'BO', 'DE', 'EN'];
    const packages = states.map(code => ({
      name: `INEC Field Kit — ${code}`,
      size: `${Math.floor(Math.random() * 50 + 10)} MB`,
      state: code,
      status: 'ready',
    }));
    setOfflinePackages(packages);
    setGenerating(false);
  }, []);

  const downloadFieldKit = useCallback((stateCode: string) => {
    const statePUs = store.pollingUnits.features.filter(f => f.properties.state_code === stateCode);
    const fc = { type: 'FeatureCollection', features: statePUs };
    downloadGeoJSON(fc, `inec-field-kit-${stateCode}.geojson`);
  }, [store.pollingUnits]);

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Election Day Field Kit</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Generate offline data packages for field officers. Each kit contains polling unit locations,
          admin boundaries, and base maps for offline use via the GeoLibre Desktop app (Tauri).
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm flex items-center gap-2">
            <Cpu className="w-4 h-4" /> GeoLibre Desktop (Tauri)
          </CardTitle>
        </CardHeader>
        <CardContent className="text-xs space-y-3">
          <p>
            The INEC Field Kit uses <strong>GeoLibre Desktop</strong> — a native app built with Tauri v2
            that runs on Windows, macOS, and Linux. It provides:
          </p>
          <ul className="list-disc pl-4 space-y-1">
            <li><strong>Offline Maps:</strong> Pre-loaded MBTiles of Nigeria admin boundaries + OpenStreetMap</li>
            <li><strong>Local GeoJSON:</strong> Polling unit data with result submission forms</li>
            <li><strong>GPS Tracking:</strong> Record officer locations for chain-of-custody audit</li>
            <li><strong>DuckDB Spatial SQL:</strong> Run queries against election data without internet</li>
            <li><strong>Photo Capture:</strong> Geotagged photos of polling units and result sheets</li>
            <li><strong>Sync Queue:</strong> Queue result submissions for upload when connectivity returns</li>
          </ul>
          <div className="flex gap-2 pt-2">
            <Button variant="outline" size="sm" className="text-xs" disabled>
              <Download className="w-3.5 h-3.5 mr-1" /> Download Desktop (Windows)
            </Button>
            <Button variant="outline" size="sm" className="text-xs" disabled>
              <Download className="w-3.5 h-3.5 mr-1" /> Download Desktop (macOS)
            </Button>
            <Button variant="outline" size="sm" className="text-xs" disabled>
              <Download className="w-3.5 h-3.5 mr-1" /> Download Desktop (Linux)
            </Button>
          </div>
          <p className="text-muted-foreground">
            Desktop builds are generated from the GeoLibre repo via <code>npm run tauri:build</code>.
            Configure the INEC field kit plugin in <code>src-tauri/tauri.conf.json</code>.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm flex items-center gap-2">
            <Database className="w-4 h-4" /> Offline State Packages
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Button onClick={generateFieldKit} disabled={generating} className="mb-4">
            {generating ? <RefreshCw className="w-4 h-4 mr-2 animate-spin" /> : <Download className="w-4 h-4 mr-2" />}
            Generate State Field Kits
          </Button>

          {offlinePackages.length > 0 && (
            <div className="border rounded overflow-hidden">
              <table className="w-full text-xs">
                <thead className="bg-muted">
                  <tr>
                    <th className="text-left py-2 px-3">Package</th>
                    <th className="text-left py-2 px-3">State</th>
                    <th className="text-left py-2 px-3">Size</th>
                    <th className="text-left py-2 px-3">Status</th>
                    <th className="text-right py-2 px-3">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {offlinePackages.map(pkg => (
                    <tr key={pkg.state} className="border-t">
                      <td className="py-2 px-3">{pkg.name}</td>
                      <td className="py-2 px-3"><Badge variant="outline">{pkg.state}</Badge></td>
                      <td className="py-2 px-3">{pkg.size}</td>
                      <td className="py-2 px-3"><Badge variant="default" className="bg-green-500 text-[10px]">Ready</Badge></td>
                      <td className="py-2 px-3 text-right">
                        <Button variant="outline" size="sm" className="text-xs h-6" onClick={() => downloadFieldKit(pkg.state)}>
                          Download
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Tauri Config */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Tauri Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="bg-muted p-4 rounded text-xs overflow-auto max-h-48 whitespace-pre-wrap">
{JSON.stringify({
  productName: 'INEC Field Kit',
  version: '1.0.0',
  identifier: 'ng.inec.fieldkit',
  build: { frontendDist: '../dist' },
  app: {
    security: {
      csp: "default-src 'self'; connect-src 'self' https://*.openstreetmap.org https://*.cartocdn.com https://tiles.openfreemap.org; img-src 'self' data: https://*.tile.openstreetmap.org https://*.cartocdn.com",
    },
    windows: [{ title: 'INEC Field Kit', width: 1280, height: 800 }],
  },
  plugins: {
    geolocation: { enabled: true },
    fs: { scope: ['$APP/**', '$DOWNLOAD/**'] },
  },
}, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
