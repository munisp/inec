import { useEffect, useRef, useState, useCallback } from 'react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { api } from '@/lib/api';
import { generateStateBoundaryGeoJSON, NIGERIA_STATE_COORDS, ZONE_COLORS } from '@/lib/nigeria-geo';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Activity, MapPin, Layers, Eye, ArrowLeft, Satellite, Map as MapIcon, Search, ExternalLink, Navigation } from 'lucide-react';

interface StateData {
  code: string; name: string; geo_zone: string; capital: string;
  total_pus: number; reported_pus: number; total_votes: number; total_cast: number;
  avg_lat: number; avg_lng: number;
  party_scores: Array<{ party_code: string; abbreviation: string; color: string; total_votes: number }>;
  leading_party: { abbreviation: string; color: string; total_votes: number } | null;
}

interface PUData {
  code: string; name: string; latitude: number; longitude: number; registered_voters: number;
  ward_name: string; lga_name: string; state_name: string; state_code: string;
  result_id: number | null; status: string | null; total_valid_votes: number | null;
  total_votes_cast: number | null; tigerbeetle_status: string | null; hyperledger_status: string | null;
  party_scores: Array<{ party_code: string; abbreviation: string; color: string; votes: number }>;
}

type MapMode = 'leading_party' | 'completion' | 'zone';
type TileMode = 'street' | 'satellite';

function formatNumber(n: number) { return new Intl.NumberFormat().format(n); }

const STATUS_COLORS: Record<string, string> = {
  finalized: '#16a34a',
  validated: '#2563eb',
  pending: '#f59e0b',
  disputed: '#dc2626',
  no_result: '#9ca3af',
};

export default function MapPage() {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const mapContainerB = useRef<HTMLDivElement>(null);
  const mapRefB = useRef<maplibregl.Map | null>(null);
  const [loading, setLoading] = useState(true);
  const [states, setStates] = useState<StateData[]>([]);
  const [pus, setPus] = useState<PUData[]>([]);
  const [selectedState, setSelectedState] = useState<StateData | null>(null);
  const [selectedPU, setSelectedPU] = useState<PUData | null>(null);
  const [mapMode, setMapMode] = useState<MapMode>('leading_party');
  const [tileMode, setTileMode] = useState<TileMode>('street');
  const [showPUs, setShowPUs] = useState(true);
  const [compareMode, setCompareMode] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [puCount, setPuCount] = useState({ total: 0, withResults: 0 });
  const [statusFilter, setStatusFilter] = useState<Record<string, boolean>>({ finalized: true, validated: true, pending: true, disputed: true, no_result: true });
  const [places, setPlaces] = useState<Array<{ name: string; lat: number; lon: number }>>([]);
  const [timeTs, setTimeTs] = useState<number | null>(null);
  const [selecting, setSelecting] = useState(false);
  const [selectionBox, setSelectionBox] = useState<{ x1: number; y1: number; x2: number; y2: number } | null>(null);
  const [selectedCodes, setSelectedCodes] = useState<string[]>([]);
  const [tileVersion, setTileVersion] = useState<number>(0);
  const [mapError, setMapError] = useState<string | null>(null);

  function sendMetric(event: string, data: any) {
    try {
      fetch(`${(import.meta as any).env.VITE_API_URL || 'http://localhost:8000'}/dashboard/metrics/client`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ event, data })
      });
    } catch {}
  }

  useEffect(() => { loadData(); }, []);

  async function loadData(stateCode?: string) {
    setLoading(true);
    try {
      const data = await api.getMapData(1, stateCode);
      setStates(data.states);
      setPus(data.polling_units);
      const withResults = data.polling_units.filter((p: PUData) => p.result_id).length;
      setPuCount({ total: data.polling_units.length, withResults });
    } catch (e) { console.error(e); }
    finally { setLoading(false); }
  }

  const getTileSource = useCallback((mode: TileMode) => {
    if (mode === 'satellite') {
      return {
        type: 'raster' as const,
        tiles: ['https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}'],
        tileSize: 256,
        attribution: '&copy; Esri, Maxar, Earthstar Geographics',
        maxzoom: 19,
      };
    }
    return {
      type: 'raster' as const,
      tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
      tileSize: 256,
      attribution: '&copy; OpenStreetMap contributors',
      maxzoom: 19,
    };
  }, []);

  useEffect(() => {
    if (loading || !mapContainer.current || states.length === 0) return;
    if (mapRef.current) {
      try { mapRef.current.remove(); } catch {}
      mapRef.current = null;
    }

    try {
    const t0 = performance.now();
    const activeStatuses = Object.entries(statusFilter).filter(([, v]) => v).map(([k]) => k);
    const puFilter: any = timeTs
      ? ['all', ['match', ['get','status'], activeStatuses, true, false],
         ['>', ['to-number', ['coalesce', ['get','submitted_ts'], 0]], 0],
         ['<=', ['to-number', ['get','submitted_ts']], timeTs]
        ]
      : ['match', ['get', 'status'], activeStatuses, true, false];

    const map = new maplibregl.Map({
      container: mapContainer.current,
      style: {
        version: 8,
        sources: { 'base-tiles': getTileSource(tileMode) },
        layers: [{
          id: 'base-tiles',
          type: 'raster',
          source: 'base-tiles',
          minzoom: 0,
          maxzoom: 19,
        }],
      },
      center: selectedState ? [NIGERIA_STATE_COORDS[selectedState.code]?.lng || 8.0, NIGERIA_STATE_COORDS[selectedState.code]?.lat || 9.0] : [8.0, 9.0],
      zoom: selectedState ? 7.5 : 5.5,
      maxBounds: [[-2, 1], [18, 16]],
    });

    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.addControl(new maplibregl.ScaleControl({ maxWidth: 200 }), 'bottom-left');
    map.addControl(new maplibregl.FullscreenControl(), 'top-right');

    map.on('load', () => {
      const t1 = performance.now();
      sendMetric('map_load', { duration_ms: Math.round(t1 - t0), tileMode, compareMode, state: selectedState?.code || null });
      const stateGeoJSON = generateStateBoundaryGeoJSON(states);
      map.addSource('states', { type: 'geojson', data: stateGeoJSON as GeoJSON.GeoJSON });

      map.addLayer({
        id: 'state-fills',
        type: 'fill',
        source: 'states',
        paint: {
          'fill-color': getStateFillExpression(mapMode),
          'fill-opacity': 0.6,
        },
      });

      map.addLayer({
        id: 'state-borders',
        type: 'line',
        source: 'states',
        paint: {
          'line-color': '#374151',
          'line-width': 1.5,
        },
      });

      map.addLayer({
        id: 'state-labels',
        type: 'symbol',
        source: 'states',
        layout: {
          'text-field': ['get', 'code'],
          'text-size': 11,
          'text-font': ['Open Sans Regular'],
          'text-allow-overlap': true,
        },
        paint: {
          'text-color': '#111827',
          'text-halo-color': '#ffffff',
          'text-halo-width': 1.5,
        },
      });

      if (showPUs) {
        map.addSource('pu-tiles', {
          type: 'vector',
          tiles: [`${(import.meta as any).env.VITE_API_URL || 'http://localhost:8000'}/geo/tiles/pus/{z}/{x}/{y}.mvt?v=${tileVersion}`],
          maxzoom: 14,
        });



        map.addLayer({
          id: 'pu-markers',
          type: 'circle',
          source: 'pu-tiles',
          filter: puFilter as any,
          paint: {
            'circle-radius': ['interpolate', ['linear'], ['zoom'], 5, 4, 8, 6, 12, 10, 15, 14],
            'circle-color': [
              'match', ['get', 'status'],
              'finalized', '#16a34a',
              'validated', '#2563eb',
              'pending', '#f59e0b',
              'disputed', '#dc2626',
              '#9ca3af'
            ],
            'circle-stroke-color': '#ffffff',
            'circle-stroke-width': 2,
            'circle-opacity': 0.9,
          },
        });

        map.addLayer({
          id: 'pu-labels',
          type: 'symbol',
          source: 'pu-tiles',
          filter: ['>=', ['zoom'], 12],
          layout: {
            'text-field': ['get', 'name'],
            'text-size': 10,
            'text-font': ['Open Sans Regular'],
            'text-offset': [0, 1.5],
            'text-anchor': 'top',
          },
          paint: {
            'text-color': tileMode === 'satellite' ? '#ffffff' : '#374151',
            'text-halo-color': tileMode === 'satellite' ? '#000000' : '#ffffff',
            'text-halo-width': 1,
          },
        });


        map.on('error', (e: any) => { try { sendMetric('map_error', { message: e?.error?.message || 'unknown', sourceId: (e as any)?.sourceId || null }); } catch {} });

        map.on('click', 'pu-markers', (e) => {
          if (!e.features || e.features.length === 0) return;
          const props = e.features[0].properties;
          const coords = [e.lngLat.lng, e.lngLat.lat] as [number, number];
          const puData = pus.find(p => p.code === props.code);
          if (puData) setSelectedPU(puData);
          const statusColor = STATUS_COLORS[props.status] || '#9ca3af';
          const statusLabel = props.status === 'no_result' ? 'No Result' : (props.status || 'N/A').charAt(0).toUpperCase() + (props.status || '').slice(1);
          const partyBars = puData?.party_scores?.slice(0, 5).map(p =>
            '<div style="display:flex;align-items:center;gap:6px;margin:2px 0">' +
              '<div style="width:10px;height:10px;border-radius:50%;background:' + p.color + ';flex-shrink:0"></div>' +
              '<span style="flex:1">' + p.abbreviation + '</span>' +
              '<span style="font-weight:600">' + formatNumber(p.votes) + '</span></div>'
          ).join('') || '<div style="color:#999">No results submitted</div>';
          const svUrl = 'https://kartaview.org/map/@' + coords[1] + ',' + coords[0] + ',17z';
          const dirUrl = 'https://www.openstreetmap.org/#map=18/' + coords[1] + '/' + coords[0];
          new maplibregl.Popup({ closeButton: true, maxWidth: '320px', className: 'pu-popup' })
            .setLngLat(coords)
            .setHTML(
              '<div style="font-family:system-ui;font-size:13px;max-height:350px;overflow-y:auto">' +
              '<div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">' +
              '<div style="width:10px;height:10px;border-radius:50%;background:' + statusColor + ';flex-shrink:0"></div>' +
              '<span style="font-weight:700;font-size:14px">' + props.name + '</span></div>' +
              '<div style="color:#666;font-size:11px;margin-bottom:8px;line-height:1.4">' +
              '<div>' + props.code + '</div>' +
              '<div>' + props.ward + ' &bull; ' + props.lga + ' &bull; ' + props.state + '</div></div>' +
              '<div style="background:#f9fafb;border-radius:6px;padding:8px;margin-bottom:8px">' +
              '<div style="display:grid;grid-template-columns:1fr 1fr;gap:4px;font-size:12px">' +
              '<span style="color:#888">Status:</span><span style="font-weight:600;color:' + statusColor + '">' + statusLabel + '</span>' +
              '<span style="color:#888">Registered:</span><span style="font-weight:600">' + formatNumber(Number(props.registered) || 0) + '</span>' +
              '<span style="color:#888">Valid Votes:</span><span style="font-weight:600">' + formatNumber(Number(props.votes) || 0) + '</span>' +
              '<span style="color:#888">Total Cast:</span><span>' + formatNumber(Number(props.cast) || 0) + '</span>' +
              '<span style="color:#888">TigerBeetle:</span><span>' + props.tb + '</span>' +
              '<span style="color:#888">Hyperledger:</span><span>' + props.hl + '</span></div></div>' +
              '<div style="margin-bottom:8px"><div style="font-weight:600;font-size:12px;margin-bottom:4px">Party Results</div>' + partyBars + '</div>' +
              '<div style="display:flex;gap:6px;border-top:1px solid #eee;padding-top:8px">' +
              '<a href="' + svUrl + '" target="_blank" rel="noopener" style="flex:1;display:flex;align-items:center;justify-content:center;gap:4px;padding:6px 8px;background:#16a34a;color:white;border-radius:6px;text-decoration:none;font-size:11px;font-weight:600">KartaView</a>' +
              '<a href="' + dirUrl + '" target="_blank" rel="noopener" style="flex:1;display:flex;align-items:center;justify-content:center;gap:4px;padding:6px 8px;background:#2563eb;color:white;border-radius:6px;text-decoration:none;font-size:11px;font-weight:600">Open in OSM</a></div>' +
              '<div style="font-size:10px;color:#aaa;margin-top:6px;text-align:center">' + coords[1].toFixed(6) + ', ' + coords[0].toFixed(6) + '</div></div>'
            ).addTo(map);
        });

        map.on('mouseenter', 'pu-markers', () => { map.getCanvas().style.cursor = 'pointer'; });
        map.on('mouseleave', 'pu-markers', () => { map.getCanvas().style.cursor = ''; });
      }

      map.on('click', 'state-fills', (e) => {
        if (!e.features || e.features.length === 0) return;
        const props = e.features[0].properties;
        const stateData = states.find(s => s.code === props.code);
        if (stateData) {
          setSelectedState(stateData);
          setSelectedPU(null);
          const coord = NIGERIA_STATE_COORDS[stateData.code];
          if (coord) {
            map.flyTo({ center: [coord.lng, coord.lat], zoom: 7.5, duration: 1000 });
            loadData(stateData.code);
          }
        }
      });

      map.on('mouseenter', 'state-fills', (e) => {
        map.getCanvas().style.cursor = 'pointer';
        if (!e.features || e.features.length === 0) return;
        const props = e.features[0].properties;
        const html = `<b>${props.name}</b><br/>${props.geo_zone}<br/>Reported: ${props.reported_pus}/${props.total_pus} (${props.completion}%)<br/>Leading: <b style="color:${props.leading_color}">${props.leading_party}</b>`;
        new maplibregl.Popup({ closeButton: false, closeOnClick: false, className: 'state-hover-popup' })
          .setLngLat(e.lngLat)
          .setHTML(`<div style="font-family:system-ui;font-size:12px">${html}</div>`)
          .addTo(map);
      });

      map.on('mouseleave', 'state-fills', () => {
        map.getCanvas().style.cursor = '';
        document.querySelectorAll('.state-hover-popup').forEach(el => el.remove());
      });
    });

    mapRef.current = map;
    return () => { try { map.remove(); } catch {} };
    } catch (err) {
      console.error('Map init error:', err);
      setMapError('Map could not be initialized. Please ensure WebGL is enabled.');
    }
  }, [loading, states, pus, mapMode, showPUs, tileMode, getTileSource, selectedState, statusFilter, timeTs, tileVersion]);

  // Compare mode: initialize second, independent map instance
  useEffect(() => {
    if (!compareMode) return;
    if (loading || !mapContainerB.current || states.length === 0) return;
    if (mapRefB.current) {
      try { mapRefB.current.remove(); } catch {}
      mapRefB.current = null;
    }
    try {

    const activeStatuses = Object.entries(statusFilter).filter(([, v]) => v).map(([k]) => k);
    const puFilterB: any = timeTs
      ? ['all', ['match', ['get','status'], activeStatuses, true, false],
         ['>', ['to-number', ['coalesce', ['get','submitted_ts'], 0]], 0],
         ['<=', ['to-number', ['get','submitted_ts']], timeTs]
        ]
      : ['match', ['get', 'status'], activeStatuses, true, false];

    const mapB = new maplibregl.Map({
      container: mapContainerB.current,
      style: {
        version: 8,
        sources: { 'base-tiles-b': getTileSource(tileMode) },
        layers: [{ id: 'base-tiles-b', type: 'raster', source: 'base-tiles-b', minzoom: 0, maxzoom: 19 }],
      },
      center: selectedState ? [NIGERIA_STATE_COORDS[selectedState.code]?.lng || 8.0, NIGERIA_STATE_COORDS[selectedState.code]?.lat || 9.0] : [8.0, 9.0],
      zoom: selectedState ? 7.5 : 5.5,
      maxBounds: [[-2, 1], [18, 16]],
    });

    mapB.addControl(new maplibregl.NavigationControl(), 'top-right');
    mapB.addControl(new maplibregl.ScaleControl({ maxWidth: 200 }), 'bottom-left');
    mapB.addControl(new maplibregl.FullscreenControl(), 'top-right');

    mapB.on('load', () => {
      const stateGeoJSON = generateStateBoundaryGeoJSON(states);
      mapB.addSource('states-b', { type: 'geojson', data: stateGeoJSON as GeoJSON.GeoJSON });

      mapB.addLayer({ id: 'state-fills-b', type: 'fill', source: 'states-b', paint: { 'fill-color': getStateFillExpression(mapMode), 'fill-opacity': 0.6 } });
      mapB.addLayer({ id: 'state-borders-b', type: 'line', source: 'states-b', paint: { 'line-color': '#374151', 'line-width': 1.5 } });
      mapB.addLayer({ id: 'state-labels-b', type: 'symbol', source: 'states-b', layout: { 'text-field': ['get', 'code'], 'text-size': 11, 'text-font': ['Open Sans Regular'], 'text-allow-overlap': true }, paint: { 'text-color': '#111827', 'text-halo-color': '#ffffff', 'text-halo-width': 1.5 } });

      if (showPUs) {
        mapB.addSource('pu-tiles-b', { type: 'vector', tiles: [`${(import.meta as any).env.VITE_API_URL || 'http://localhost:8000'}/geo/tiles/pus/{z}/{x}/{y}.mvt?v=${tileVersion}`], maxzoom: 14 });
        mapB.addLayer({ id: 'pu-markers-b', type: 'circle', source: 'pu-tiles-b', filter: puFilterB as any, paint: {
          'circle-radius': ['interpolate', ['linear'], ['zoom'], 5, 4, 8, 6, 12, 10, 15, 14],
          'circle-color': ['match', ['get', 'status'], 'finalized', '#16a34a', 'validated', '#2563eb', 'pending', '#f59e0b', 'disputed', '#dc2626', '#9ca3af'],
          'circle-stroke-color': '#ffffff','circle-stroke-width': 2,'circle-opacity': 0.9 } });
        mapB.addLayer({ id: 'pu-labels-b', type: 'symbol', source: 'pu-tiles-b', filter: ['>=', ['zoom'], 12], layout: { 'text-field': ['get', 'name'], 'text-size': 10, 'text-font': ['Open Sans Regular'], 'text-offset': [0, 1.5], 'text-anchor': 'top' }, paint: { 'text-color': tileMode === 'satellite' ? '#ffffff' : '#374151', 'text-halo-color': tileMode === 'satellite' ? '#000000' : '#ffffff', 'text-halo-width': 1 } });

        mapB.on('mouseenter', 'pu-markers-b', () => { mapB.getCanvas().style.cursor = 'pointer'; });
        mapB.on('mouseleave', 'pu-markers-b', () => { mapB.getCanvas().style.cursor = ''; });
      }
    });

    mapRefB.current = mapB;
    return () => { try { mapB.remove(); } catch {} };
    } catch (err) { console.error('Compare map init error:', err); }
  }, [compareMode, loading, states, pus, mapMode, showPUs, tileMode, getTileSource, selectedState, statusFilter, timeTs, tileVersion]);

  function getStateFillExpression(mode: MapMode): maplibregl.ExpressionSpecification {
    if (mode === 'leading_party') {
      return ['get', 'leading_color'] as unknown as maplibregl.ExpressionSpecification;
    } else if (mode === 'completion') {
      return [
        'interpolate', ['linear'], ['get', 'completion'],
        0, '#fee2e2', 25, '#fde68a', 50, '#bbf7d0', 75, '#86efac', 100, '#16a34a'
      ] as unknown as maplibregl.ExpressionSpecification;
    } else {
      const zoneEntries: (string | string[])[] = [];
      for (const [zone, color] of Object.entries(ZONE_COLORS)) {
        zoneEntries.push(zone, color);
      }
      return ['match', ['get', 'geo_zone'], ...zoneEntries.flat(), '#cccccc'] as unknown as maplibregl.ExpressionSpecification;
    }
  }

  function resetView() {
    setSelectedState(null);
    setSelectedPU(null);
    loadData();
    if (mapRef.current) {
      mapRef.current.flyTo({ center: [8.0, 9.0], zoom: 5.5, duration: 1000 });
    }
  }

  function flyToPU(pu: PUData) {
    setSelectedPU(pu);
    if (mapRef.current) {
      mapRef.current.flyTo({ center: [pu.longitude, pu.latitude], zoom: 14, duration: 1200 });
    }
  }

  const filteredPUs = searchQuery.trim()
    ? pus.filter(p =>
        p.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        p.code.toLowerCase().includes(searchQuery.toLowerCase()) ||
        p.lga_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        p.ward_name.toLowerCase().includes(searchQuery.toLowerCase())
      ).slice(0, 20)
    : [];

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

  useEffect(() => {
    try {
      const base = (import.meta as any).env.VITE_API_URL || 'http://localhost:8000';
      const wsUrl = base.replace(/^http/, 'ws') + '/results/ws/updates';
      const ws = new WebSocket(wsUrl);
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data);
          if (msg && msg.type === 'result_updated') {
            setTileVersion(Date.now());
          }
        } catch {}
      };
      return () => { try { ws.close(); } catch {} };
    } catch {}
  }, []);

  if (loading && states.length === 0) {
    return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;
  }

  function flyToCoords(lat: number, lon: number, zoom = 14) {
    if (mapRef.current) {
      mapRef.current.flyTo({ center: [lon, lat], zoom, duration: 1200 });
    }
  }

  // Selection overlay handlers (box select on primary map)
  function onSelectMouseDown(e: React.MouseEvent<HTMLDivElement>) {
    if (!selecting || !mapRef.current) return;
    const rect = (mapRef.current.getContainer() as HTMLDivElement).getBoundingClientRect();
    setSelectionBox({ x1: e.clientX - rect.left, y1: e.clientY - rect.top, x2: e.clientX - rect.left, y2: e.clientY - rect.top });
  }
  function onSelectMouseMove(e: React.MouseEvent<HTMLDivElement>) {
    if (!selecting || !mapRef.current || !selectionBox) return;
    const rect = (mapRef.current.getContainer() as HTMLDivElement).getBoundingClientRect();
    setSelectionBox({ ...selectionBox, x2: e.clientX - rect.left, y2: e.clientY - rect.top });
  }
  function onSelectMouseUp() {
    if (!selecting || !mapRef.current || !selectionBox) { setSelectionBox(null); return; }
    const minX = Math.min(selectionBox.x1, selectionBox.x2);
    const minY = Math.min(selectionBox.y1, selectionBox.y2);
    const maxX = Math.max(selectionBox.x1, selectionBox.x2);
    const maxY = Math.max(selectionBox.y1, selectionBox.y2);
    const bbox: [maplibregl.PointLike, maplibregl.PointLike] = ([{ x: minX, y: minY } as any, { x: maxX, y: maxY } as any]);
    const feats = mapRef.current.queryRenderedFeatures(bbox, { layers: ['pu-markers'] });
    const codes = Array.from(new Set(feats.map(f => (f.properties as any)?.code).filter(Boolean)));
    setSelectedCodes(codes as string[]);
    setSelecting(false);
    setSelectionBox(null);
  }

  function exportSelectionCSV() {
    const cols = ['code','name','lga','state','status','registered_voters','total_valid_votes','total_votes_cast','latitude','longitude'];
    const rows = pus.filter(p => selectedCodes.includes(p.code)).map(p => [p.code,p.name,p.lga_name,p.state_name,p.status||'no_result',p.registered_voters,p.total_valid_votes||0,p.total_votes_cast||0,p.latitude,p.longitude]);
    const csv = [cols.join(','), ...rows.map(r => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a'); a.href = url; a.download = 'selection.csv'; a.click(); URL.revokeObjectURL(url);
  }
  function exportSelectionGeoJSON() {
    const features = pus.filter(p => selectedCodes.includes(p.code)).map(p => ({ type: 'Feature', geometry: { type: 'Point', coordinates: [p.longitude, p.latitude] }, properties: { code: p.code, name: p.name, status: p.status } }));
    const gj = { type: 'FeatureCollection', features } as any;
    const blob = new Blob([JSON.stringify(gj)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a'); a.href = url; a.download = 'selection.geojson'; a.click(); URL.revokeObjectURL(url);
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          {selectedState && (
            <Button variant="ghost" size="sm" onClick={resetView} className="gap-1">
              <ArrowLeft className="w-4 h-4" /> National View
            </Button>
          )}
          <Badge variant="outline" className="gap-1">
            <MapPin className="w-3 h-3" />
            {selectedState ? `${selectedState.name} — ${puCount.total} PUs (${puCount.withResults} reported)` : `37 States — ${puCount.total} PUs shown`}
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-md border overflow-hidden">
            <Button variant={tileMode === 'street' ? 'default' : 'ghost'} size="sm" onClick={() => setTileMode('street')} className="rounded-none gap-1 h-8">
              <MapIcon className="w-3.5 h-3.5" /> Street
            </Button>
            <Button variant={tileMode === 'satellite' ? 'default' : 'ghost'} size="sm" onClick={() => setTileMode('satellite')} className="rounded-none gap-1 h-8">
              <Satellite className="w-3.5 h-3.5" /> Satellite
            </Button>
          </div>
          <Button variant={compareMode ? 'default' : 'outline'} size="sm" onClick={() => setCompareMode(!compareMode)} className="gap-1 h-8">
            Compare
          </Button>
          <Select value={mapMode} onValueChange={(v) => setMapMode(v as MapMode)}>
            <SelectTrigger className="w-40 h-8">
              <Layers className="w-3.5 h-3.5 mr-1" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="leading_party">Leading Party</SelectItem>
              <SelectItem value="completion">Completion %</SelectItem>
              <SelectItem value="zone">Geo-Political Zone</SelectItem>
            </SelectContent>
          </Select>
          <Button variant={showPUs ? 'default' : 'outline'} size="sm" onClick={() => setShowPUs(!showPUs)} className="gap-1 h-8">
            <Eye className="w-3.5 h-3.5" /> PU Markers
          </Button>
          <Button variant={selecting ? 'default' : 'outline'} size="sm" onClick={() => { setSelecting(v => !v); setSelectionBox(null); }} className="gap-1 h-8" aria-label="Toggle box select">
            Box Select
          </Button>
          <Button variant="outline" size="sm" onClick={() => {
            const base = `${(import.meta as any).env.VITE_API_URL || 'http://localhost:8000'}/geo/reports/polling-units.csv?election_id=1${selectedState ? `&state_code=${selectedState.code}` : ''}`;
            window.open(base, '_blank');
          }} className="gap-1 h-8" aria-label="Export polling units CSV">
            Export CSV
          </Button>
          <Button variant="outline" size="sm" onClick={() => {
            const base = `${(import.meta as any).env.VITE_API_URL || 'http://localhost:8000'}/geo/reports/polling-units.geojson?election_id=1${selectedState ? `&state_code=${selectedState.code}` : ''}`;
            window.open(base, '_blank');
          }} className="gap-1 h-8" aria-label="Export polling units GeoJSON">
            Export GeoJSON
          </Button>
          <div className="hidden lg:flex items-center gap-2 ml-2">
            <input type="range" min={0} max={Math.floor(Date.now()/1000)} value={timeTs ?? Math.floor(Date.now()/1000)} onChange={(e)=>{
              const now = Math.floor(Date.now()/1000);
              const v = Number(e.target.value);
              setTimeTs(v >= now ? null : v);
            }} className="w-56" />
            <span className="text-xs text-zinc-500 w-36 truncate">
              {(new Date(((timeTs ?? Math.floor(Date.now()/1000)))*1000)).toLocaleString()}
            </span>
          </div>
        </div>
      </div>

      <div className="grid lg:grid-cols-4 gap-4">
        <div className="lg:col-span-3">
          <Card className="overflow-hidden">
            <CardContent className="p-0 relative">
              {mapError ? (
                <div className="flex flex-col items-center justify-center" style={{ height: '650px' }}>
                  <MapIcon className="w-12 h-12 text-zinc-300 mb-3" />
                  <p className="text-zinc-500 text-sm">{mapError}</p>
                  <Button variant="outline" size="sm" className="mt-3" onClick={() => { setMapError(null); }}>Retry</Button>
                </div>
              ) : compareMode ? (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-2 relative">
                  <div ref={mapContainer} className="w-full relative" style={{ height: '650px' }} />
                  <div ref={mapContainerB} className="w-full" style={{ height: '650px' }} />
                  {/* Selection overlay over primary map */}
                  <div className="absolute inset-y-0 left-0" style={{ width: '50%', pointerEvents: selecting ? 'auto' : 'none' }}
                    onMouseDown={onSelectMouseDown} onMouseMove={onSelectMouseMove} onMouseUp={onSelectMouseUp} />
                  {selectionBox && (
                    <div className="absolute border-2 border-blue-500 bg-blue-500/10" style={{
                      left: Math.min(selectionBox.x1, selectionBox.x2),
                      top: Math.min(selectionBox.y1, selectionBox.y2),
                      width: Math.abs(selectionBox.x2 - selectionBox.x1),
                      height: Math.abs(selectionBox.y2 - selectionBox.y1),
                    }} />
                  )}
                </div>
              ) : (
                <div className="relative">
                  <div ref={mapContainer} className="w-full" style={{ height: '650px' }} />
                  <div className="absolute inset-0" style={{ pointerEvents: selecting ? 'auto' : 'none' }}
                    onMouseDown={onSelectMouseDown} onMouseMove={onSelectMouseMove} onMouseUp={onSelectMouseUp} />
                  {selectionBox && (
                    <div className="absolute border-2 border-blue-500 bg-blue-500/10" style={{
                      left: Math.min(selectionBox.x1, selectionBox.x2),
                      top: Math.min(selectionBox.y1, selectionBox.y2),
                      width: Math.abs(selectionBox.x2 - selectionBox.x1),
                      height: Math.abs(selectionBox.y2 - selectionBox.y1),
                    }} />
                  )}
                </div>
              )}
              <div className="absolute bottom-8 right-2 flex flex-col gap-1">
                {Object.entries(STATUS_COLORS).map(([status, color]) => (
                  <div key={status} className="flex items-center gap-1.5 bg-white/90 rounded px-2 py-0.5 text-xs shadow">
                    <div className="w-3 h-3 rounded-full border border-white" style={{ backgroundColor: color }} />
                    <span className="capitalize">{status === 'no_result' ? 'No Result' : status}</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>

        <div className="space-y-3">
          <Card>
            <CardContent className="p-3">
              <p className="text-xs text-zinc-500 mb-2">Filter by status</p>
              <div className="flex flex-wrap gap-1">
                {Object.entries(STATUS_COLORS).map(([s, color]) => (
                  <Button key={s} size="sm" variant={statusFilter[s as keyof typeof statusFilter] ? 'default' : 'outline'}
                    onClick={() => setStatusFilter(prev => ({ ...prev, [s]: !prev[s as keyof typeof prev] }))}
                    className="h-7 px-2 text-xs"
                    style={statusFilter[s as keyof typeof statusFilter] ? { backgroundColor: color, borderColor: color } : {}}>
                    <span className="capitalize">{s === 'no_result' ? 'No Result' : s}</span>
                  </Button>
                ))}
              </div>
            </CardContent>
          </Card>

          {selectedCodes.length > 0 && (
            <Card className="border-blue-200">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Selection ({selectedCodes.length} PUs)</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex gap-2">
                  <Button size="sm" onClick={exportSelectionCSV} className="h-7">Export CSV</Button>
                  <Button size="sm" variant="outline" onClick={exportSelectionGeoJSON} className="h-7">Export GeoJSON</Button>
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardContent className="p-3">
              <div className="relative">
                <Search className="absolute left-2.5 top-2 w-4 h-4 text-zinc-400" />
                <Input
                  placeholder="Search polling units..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-8 h-8 text-sm"
                />
              </div>
              {filteredPUs.length > 0 && (
                <div className="mt-2 max-h-48 overflow-y-auto space-y-0.5">
                  {filteredPUs.map(pu => (
                    <div key={pu.code}
                      className="flex items-center gap-2 text-xs p-1.5 rounded cursor-pointer hover:bg-zinc-100"
                      onClick={() => flyToPU(pu)}>
                      <div className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ backgroundColor: STATUS_COLORS[pu.status || 'no_result'] }} />
                      <div className="min-w-0">
                        <div className="font-medium truncate">{pu.name}</div>
                        <div className="text-zinc-400 truncate">{pu.lga_name}, {pu.state_name}</div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              {places.length > 0 && (
                <div className="mt-2 border-t pt-2">
                  <p className="text-xs text-zinc-500 mb-1">Places</p>
                  <div className="max-h-40 overflow-y-auto space-y-0.5">
                    {places.map((pl, idx) => (
                      <div key={idx}
                        className="flex items-center gap-2 text-xs p-1.5 rounded cursor-pointer hover:bg-zinc-100"
                        onClick={() => { setSelectedState(null); setSelectedPU(null); flyToCoords(pl.lat, pl.lon, 14); }}>
                        <MapPin className="w-3 h-3 text-zinc-400" />
                        <div className="truncate">{pl.name}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          {selectedPU ? (
            <Card className="border-green-200">
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm">{selectedPU.name}</CardTitle>
                  <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => setSelectedPU(null)}>x</Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="text-xs space-y-1">
                  <p className="text-zinc-500">{selectedPU.code}</p>
                  <p className="text-zinc-500">{selectedPU.ward_name}, {selectedPU.lga_name}, {selectedPU.state_name}</p>
                  <div className="flex justify-between"><span className="text-zinc-500">Status:</span>
                    <Badge style={{ backgroundColor: STATUS_COLORS[selectedPU.status || 'no_result'], color: 'white' }} className="text-xs capitalize">
                      {selectedPU.status || 'No Result'}
                    </Badge>
                  </div>
                  <div className="flex justify-between"><span className="text-zinc-500">Registered:</span><span className="font-medium">{formatNumber(selectedPU.registered_voters)}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Valid Votes:</span><span className="font-bold">{formatNumber(selectedPU.total_valid_votes || 0)}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Total Cast:</span><span>{formatNumber(selectedPU.total_votes_cast || 0)}</span></div>
                </div>
                {selectedPU.party_scores.length > 0 && (
                  <div className="border-t pt-2">
                    <p className="text-xs font-medium mb-1">Party Scores</p>
                    {selectedPU.party_scores.map(p => (
                      <div key={p.party_code} className="flex items-center justify-between text-xs py-0.5">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: p.color }} />
                          <span>{p.abbreviation}</span>
                        </div>
                        <span className="font-medium">{formatNumber(p.votes)}</span>
                      </div>
                    ))}
                  </div>
                )}
                <div className="flex gap-2 pt-2">
                  <a href={`https://kartaview.org/map/@${selectedPU.latitude},${selectedPU.longitude},17z`}
                    target="_blank" rel="noopener noreferrer"
                    className="flex-1 flex items-center justify-center gap-1 px-2 py-1.5 bg-green-600 text-white rounded text-xs font-medium hover:bg-green-700">
                    <ExternalLink className="w-3 h-3" /> Street View
                  </a>
                  <a href={`https://www.openstreetmap.org/#map=18/${selectedPU.latitude}/${selectedPU.longitude}`}
                    target="_blank" rel="noopener noreferrer"
                    className="flex-1 flex items-center justify-center gap-1 px-2 py-1.5 bg-blue-600 text-white rounded text-xs font-medium hover:bg-blue-700">
                    <Navigation className="w-3 h-3" /> Open in OSM
                  </a>
                </div>
                <div className="border-t pt-2">
                  <p className="text-xs font-medium mb-1">Street imagery (KartaView)</p>
                  <iframe
                    title="KartaView street imagery"
                    src={`https://kartaview.org/map/@${selectedPU.latitude},${selectedPU.longitude},17z`}
                    className="w-full rounded border"
                    style={{ height: '220px' }}
                    loading="lazy"
                    referrerPolicy="no-referrer-when-downgrade"
                  />
                </div>
              </CardContent>
            </Card>
          ) : selectedState ? (
            <Card className="border-green-200">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">{selectedState.name}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="text-xs space-y-1">
                  <div className="flex justify-between"><span className="text-zinc-500">Zone:</span><Badge variant="outline" className="text-xs">{selectedState.geo_zone}</Badge></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Capital:</span><span>{selectedState.capital}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Polling Units:</span><span>{formatNumber(selectedState.total_pus)}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Reported:</span><span>{formatNumber(selectedState.reported_pus)}</span></div>
                  <div className="flex justify-between"><span className="text-zinc-500">Total Votes:</span><span className="font-bold">{formatNumber(selectedState.total_votes)}</span></div>
                </div>
                <div className="border-t pt-2">
                  <p className="text-xs font-medium mb-2">Party Scores</p>
                  {selectedState.party_scores.slice(0, 6).map(p => (
                    <div key={p.party_code} className="flex items-center justify-between text-xs py-0.5">
                      <div className="flex items-center gap-1.5">
                        <div className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: p.color }} />
                        <span>{p.abbreviation}</span>
                      </div>
                      <span className="font-medium">{formatNumber(p.total_votes)}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Map Legend</CardTitle>
              </CardHeader>
              <CardContent>
                {mapMode === 'leading_party' && (
                  <div className="space-y-1.5">
                    <p className="text-xs text-zinc-500 mb-2">States colored by leading party</p>
                    {(() => {
                      const seen = new Map<string, string>();
                      const counts = new Map<string, number>();
                      states.forEach(s => {
                        if (s.leading_party) {
                          seen.set(s.leading_party.abbreviation, s.leading_party.color);
                          counts.set(s.leading_party.abbreviation, (counts.get(s.leading_party.abbreviation) || 0) + 1);
                        }
                      });
                      return Array.from(seen.entries()).map(([abbr, color]) => (
                        <div key={abbr} className="flex items-center justify-between text-xs">
                          <div className="flex items-center gap-2">
                            <div className="w-4 h-3 rounded" style={{ backgroundColor: color }} />
                            <span>{abbr}</span>
                          </div>
                          <span className="text-zinc-400">{counts.get(abbr)} states</span>
                        </div>
                      ));
                    })()}
                  </div>
                )}
                {mapMode === 'completion' && (
                  <div className="space-y-1.5">
                    <p className="text-xs text-zinc-500 mb-2">Results reporting completion</p>
                    {[{ label: '0%', color: '#fee2e2' }, { label: '25%', color: '#fde68a' }, { label: '50%', color: '#bbf7d0' }, { label: '75%', color: '#86efac' }, { label: '100%', color: '#16a34a' }].map(l => (
                      <div key={l.label} className="flex items-center gap-2 text-xs">
                        <div className="w-4 h-3 rounded" style={{ backgroundColor: l.color }} />
                        <span>{l.label}</span>
                      </div>
                    ))}
                  </div>
                )}
                {mapMode === 'zone' && (
                  <div className="space-y-1.5">
                    <p className="text-xs text-zinc-500 mb-2">Geo-political zones</p>
                    {Object.entries(ZONE_COLORS).map(([zone, color]) => (
                      <div key={zone} className="flex items-center gap-2 text-xs">
                        <div className="w-4 h-3 rounded" style={{ backgroundColor: color }} />
                        <span>{zone}</span>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Top States by Votes</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-1">
                {[...states].sort((a, b) => b.total_votes - a.total_votes).slice(0, 8).map(s => (
                  <div key={s.code} className="flex items-center justify-between text-xs py-0.5 cursor-pointer hover:bg-zinc-50 px-1 rounded"
                    onClick={() => {
                      setSelectedState(s);
                      setSelectedPU(null);
                      loadData(s.code);
                      const coord = NIGERIA_STATE_COORDS[s.code];
                      if (coord && mapRef.current) {
                        mapRef.current.flyTo({ center: [coord.lng, coord.lat], zoom: 7.5, duration: 1000 });
                      }
                    }}>
                    <span className="flex items-center gap-1.5">
                      <div className="w-2 h-2 rounded-full" style={{ backgroundColor: s.leading_party?.color || '#ccc' }} />
                      {s.name}
                    </span>
                    <span className="font-medium">{formatNumber(s.total_votes)}</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
