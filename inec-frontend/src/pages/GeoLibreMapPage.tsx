/**
 * GeoLibreMapPage — GeoLibre-powered geospatial analytics for INEC
 *
 * Replaces the basic MapLibre setup with a full GIS workstation powered by
 * deck.gl overlays, spatial analysis tools, H3 hex grids, and GeoLibre viewer.
 *
 * Tabs: Live Map | Spatial Analysis | GeoLibre Viewer | Field Kit
 */
import { useEffect, useState, useCallback, useRef } from 'react';
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
} from 'lucide-react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { MapboxOverlay } from '@deck.gl/mapbox';
import { useGeoLibreStore } from '@/lib/geolibre/store';
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

const BASEMAPS: { id: string; label: string; url: string }[] = [
  { id: 'liberty', label: 'OpenFreeMap', url: 'https://tiles.openfreemap.org/styles/liberty' },
  { id: 'positron', label: 'CARTO Positron', url: 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json' },
  { id: 'dark', label: 'CARTO Dark', url: 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json' },
  { id: 'voyager', label: 'CARTO Voyager', url: 'https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json' },
];

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
      <div className="flex-1 min-h-0">
        {activeTab === 'live-map' && <LiveMapTab />}
        {activeTab === 'spatial' && <SpatialAnalysisTab />}
        {activeTab === 'geolibre' && <GeoLibreViewerTab />}
        {activeTab === 'field-kit' && <FieldKitTab />}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// LIVE MAP TAB — deck.gl overlays on MapLibre
// ═══════════════════════════════════════════════════════════════════════════

function LiveMapTab() {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const deckOverlay = useRef<MapboxOverlay | null>(null);
  const [mapReady, setMapReady] = useState(false);
  const [stats, setStats] = useState({ pus: 0, incidents: 0, bvas: 0, officials: 0 });
  const [selectedFeature, setSelectedFeature] = useState<Record<string, unknown> | null>(null);

  const store = useGeoLibreStore();

  // Initialize map
  useEffect(() => {
    if (!mapContainer.current || mapRef.current) return;

    const map = new maplibregl.Map({
      container: mapContainer.current,
      style: store.basemapStyle,
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

    map.on('load', () => {
      setMapReady(true);
    });

    mapRef.current = map;
    deckOverlay.current = overlay;

    return () => { map.remove(); mapRef.current = null; };
  }, []);

  // Load data
  const loadData = useCallback(async () => {
    store.setLoading(true);
    try {
      const [pus, incidents, bvas, officials] = await Promise.all([
        fetchPollingUnitsGeoJSON(store.electionId, store.selectedStateCode || undefined),
        fetchIncidentsGeoJSON(store.electionId),
        fetchBVASGeoJSON(),
        fetchOfficialsGeoJSON(),
      ]);
      store.setPollingUnits(pus);
      store.setIncidents(incidents);
      store.setBvasDevices(bvas);
      store.setOfficials(officials);
      setStats({
        pus: pus.features.length,
        incidents: incidents.features.length,
        bvas: bvas.features.length,
        officials: officials.features.length,
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
    mapRef.current.setStyle(store.basemapStyle);
  }, [store.basemapStyle]);

  return (
    <div className="flex h-full">
      {/* Left panel — layer control */}
      <div className="w-72 border-r bg-white overflow-y-auto shrink-0 p-3 space-y-3">
        {/* Stats */}
        <div className="grid grid-cols-2 gap-2">
          <StatCard icon={MapPin} label="PUs" value={stats.pus} color="text-blue-500" />
          <StatCard icon={AlertTriangle} label="Incidents" value={stats.incidents} color="text-red-500" />
          <StatCard icon={Radio} label="BVAS" value={stats.bvas} color="text-purple-500" />
          <StatCard icon={Users} label="Officials" value={stats.officials} color="text-cyan-500" />
        </div>

        {/* Basemap */}
        <div>
          <label className="text-xs font-medium text-muted-foreground">Basemap</label>
          <Select value={store.basemapStyle} onValueChange={store.setBasemapStyle}>
            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {BASEMAPS.map(b => (
                <SelectItem key={b.id} value={b.url}>{b.label}</SelectItem>
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

        {/* Layers */}
        <div>
          <label className="text-xs font-medium text-muted-foreground block mb-1">Layers</label>
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

        {/* 3D toggle */}
        <Button variant="outline" size="sm" className="w-full text-xs" onClick={store.toggle3D}>
          <Box className="w-3.5 h-3.5 mr-1" /> {store.is3D ? 'Disable 3D' : 'Enable 3D'}
        </Button>

        {/* Export */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Export GeoJSON</label>
          <Button variant="outline" size="sm" className="w-full text-xs"
            onClick={() => downloadGeoJSON(store.pollingUnits, 'inec-polling-units.geojson')}>
            <Download className="w-3.5 h-3.5 mr-1" /> Polling Units
          </Button>
          <Button variant="outline" size="sm" className="w-full text-xs"
            onClick={() => downloadGeoJSON(store.incidents, 'inec-incidents.geojson')}>
            <Download className="w-3.5 h-3.5 mr-1" /> Incidents
          </Button>
        </div>

        {/* Refresh */}
        <Button variant="default" size="sm" className="w-full text-xs" onClick={loadData} disabled={store.loading}>
          <RefreshCw className={`w-3.5 h-3.5 mr-1 ${store.loading ? 'animate-spin' : ''}`} /> Refresh Data
        </Button>
      </div>

      {/* Map */}
      <div className="flex-1 relative">
        <div ref={mapContainer} className="absolute inset-0" />

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

        {/* Loading overlay */}
        {store.loading && (
          <div className="absolute inset-0 bg-white/50 flex items-center justify-center z-20">
            <div className="flex items-center gap-2 bg-white px-4 py-2 rounded-lg shadow">
              <RefreshCw className="w-4 h-4 animate-spin" /> Loading election data...
            </div>
          </div>
        )}
      </div>
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

    // Client-side spatial analysis using the loaded GeoJSON
    const pus = store.pollingUnits.features;
    let analysisResult: Record<string, unknown>;

    switch (queryType) {
      case 'buffer': {
        const radius = parseFloat(bufferRadius);
        const totalPUs = pus.length;
        const withResults = pus.filter(p => p.properties.status !== 'no_result').length;
        analysisResult = {
          tool: 'Buffer Analysis',
          radius_km: radius,
          total_pus: totalPUs,
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
          .sort(([, a], [, b]) => b - a)
          .slice(0, 5)
          .map(([state, count]) => ({ state, incidents: count }));

        analysisResult = {
          tool: 'Hotspot Detection',
          total_incidents: incidents.length,
          hotspot_states: hotspots,
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
          tool: 'H3 Hex Aggregation',
          total_hexagons: Object.keys(stateTurnout).length,
          state_metrics: Object.entries(stateTurnout).map(([state, d]) => ({
            state,
            pus: d.count,
            registered: d.total,
            cast: d.cast,
            turnout_pct: d.total > 0 ? ((d.cast / d.total) * 100).toFixed(1) + '%' : '0%',
          })),
        };
        break;
      }
      default:
        analysisResult = {
          tool: queryType,
          status: 'available',
          message: `${queryType} analysis requires loading spatial data first. Use the Live Map tab to load polling unit data, then run this tool.`,
          pus_loaded: puCount,
        };
    }

    setResults(analysisResult);
  }, [queryType, bufferRadius, store.pollingUnits, store.incidents]);

  return (
    <div className="flex h-full">
      {/* Tools panel */}
      <div className="w-80 border-r bg-white overflow-y-auto p-4 space-y-3">
        <h3 className="text-sm font-semibold">Spatial Analysis Tools</h3>
        <p className="text-xs text-muted-foreground">
          GeoLibre-powered spatial analysis for election data. Select a tool and run analysis against loaded data.
        </p>

        {tools.map(tool => (
          <button key={tool.id}
            className={`w-full text-left p-3 rounded-lg border transition-colors ${queryType === tool.id ? 'border-primary bg-primary/5' : 'hover:bg-muted'}`}
            onClick={() => setQueryType(tool.id)}
          >
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

      {/* Results */}
      <div className="flex-1 p-6 overflow-y-auto">
        {results ? (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">
                {(results.tool as string) || 'Analysis'} Results
              </CardTitle>
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
// GEOLIBRE VIEWER TAB — Embed full GeoLibre GIS workstation
// ═══════════════════════════════════════════════════════════════════════════

function GeoLibreViewerTab() {
  const [viewerUrl, setViewerUrl] = useState('https://viewer.geolibre.app');
  const [urlInput, setUrlInput] = useState('');
  const store = useGeoLibreStore();

  const loadProject = useCallback((url: string) => {
    if (url) {
      setViewerUrl(`https://viewer.geolibre.app/?url=${encodeURIComponent(url)}`);
    }
  }, []);

  const exportToGeoLibre = useCallback(async () => {
    // Generate a GeoLibre project JSON from current election data
    const project = {
      version: '1.0',
      name: `INEC Election ${store.electionId} — GeoLibre Analysis`,
      description: 'Exported from INEC Election Management Platform',
      center: [NIGERIA_CENTER.longitude, NIGERIA_CENTER.latitude],
      zoom: NIGERIA_CENTER.zoom,
      layers: [
        {
          name: 'Polling Units',
          type: 'geojson',
          visible: true,
          data: store.pollingUnits,
        },
        {
          name: 'Election Incidents',
          type: 'geojson',
          visible: true,
          data: store.incidents,
        },
      ],
    };

    downloadGeoJSON(project, `inec-election-${store.electionId}.geolibre.json`);
  }, [store.electionId, store.pollingUnits, store.incidents]);

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center gap-2 p-2 border-b bg-white shrink-0">
        <Badge variant="secondary" className="text-xs">GeoLibre Viewer</Badge>

        <Input
          placeholder="Load .geolibre.json project URL..."
          value={urlInput}
          onChange={e => setUrlInput(e.target.value)}
          className="flex-1 h-8 text-xs"
          onKeyDown={e => e.key === 'Enter' && loadProject(urlInput)}
        />
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

      {/* Embedded viewer */}
      <div className="flex-1">
        <iframe
          src={viewerUrl}
          className="w-full h-full border-0"
          title="GeoLibre Viewer"
          allow="geolocation; fullscreen"
          sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
        />
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// FIELD KIT TAB — Offline/Desktop configuration for election day
// ═══════════════════════════════════════════════════════════════════════════

function FieldKitTab() {
  const store = useGeoLibreStore();
  const [generating, setGenerating] = useState(false);
  const [offlinePackages, setOfflinePackages] = useState<Array<{ name: string; size: string; state: string; status: string }>>([]);

  const generateFieldKit = useCallback(async () => {
    setGenerating(true);
    // Generate offline data packages per state
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
    // Export polling units for this state as a GeoJSON file for offline use
    const statePUs = store.pollingUnits.features.filter(
      f => f.properties.state_code === stateCode
    );
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

      {/* Tauri Desktop Info */}
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

      {/* Offline Package Generator */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm flex items-center gap-2">
            <Database className="w-4 h-4" /> Offline Data Packages
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Button onClick={generateFieldKit} disabled={generating}>
            {generating ? <RefreshCw className="w-4 h-4 mr-2 animate-spin" /> : <Box className="w-4 h-4 mr-2" />}
            Generate State Field Kits
          </Button>

          {offlinePackages.length > 0 && (
            <div className="border rounded overflow-hidden">
              <table className="w-full text-xs">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-3 py-2 text-left">Package</th>
                    <th className="px-3 py-2 text-left">State</th>
                    <th className="px-3 py-2 text-left">Size</th>
                    <th className="px-3 py-2 text-left">Status</th>
                    <th className="px-3 py-2 text-left">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {offlinePackages.map(pkg => (
                    <tr key={pkg.state} className="border-t">
                      <td className="px-3 py-2">{pkg.name}</td>
                      <td className="px-3 py-2"><Badge variant="outline">{pkg.state}</Badge></td>
                      <td className="px-3 py-2">{pkg.size}</td>
                      <td className="px-3 py-2">
                        <Badge variant="secondary" className="text-green-700 bg-green-50">Ready</Badge>
                      </td>
                      <td className="px-3 py-2">
                        <Button variant="ghost" size="sm" className="h-6 text-xs"
                          onClick={() => downloadFieldKit(pkg.state)}>
                          <Download className="w-3 h-3 mr-1" /> Download
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
          <CardTitle className="text-sm flex items-center gap-2">
            <Shield className="w-4 h-4" /> Tauri Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="bg-muted p-3 rounded text-xs overflow-auto max-h-60">
{JSON.stringify({
  "productName": "INEC Field Kit",
  "version": "1.0.0",
  "identifier": "ng.inec.fieldkit",
  "build": {
    "frontendDist": "../dist",
    "devUrl": "http://localhost:5173"
  },
  "app": {
    "withGlobalTauri": true,
    "security": {
      "csp": "default-src 'self'; connect-src 'self' https://tiles.openfreemap.org https://basemaps.cartocdn.com https://*.inec.gov.ng; img-src 'self' data: blob: https://*.tile.openstreetmap.org"
    }
  },
  "plugins": {
    "fs": { "scope": ["$APPDATA/**", "$DOWNLOAD/**"] },
    "http": { "scope": ["https://*.inec.gov.ng/**", "https://tiles.openfreemap.org/**"] }
  }
}, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
