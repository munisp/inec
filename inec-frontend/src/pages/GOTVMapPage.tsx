import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { Deck } from '@deck.gl/core';
import { ScatterplotLayer, ArcLayer } from '@deck.gl/layers';
import { H3HexagonLayer } from '@deck.gl/geo-layers';
import { HeatmapLayer } from '@deck.gl/aggregation-layers';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { api } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { MapPin, Navigation, Car, Eye, EyeOff, Radio } from 'lucide-react';
import { latLngToCell } from 'h3-js';

// ─── Types ─────────────────────────────────────────────────────────────────

interface GeoVolunteer {
  id: string;
  name: string;
  role: string;
  lat: number;
  lng: number;
  active: boolean;
  has_vehicle: boolean;
  vehicle_capacity: number;
  doors_knocked: number;
  calls_made: number;
  rides_given: number;
  last_checkin: string | null;
}

interface GeoRide {
  id: string;
  contact_id: string;
  volunteer_id: string | null;
  pu_code: string;
  pickup_lat: number;
  pickup_lng: number;
  pu_lat: number;
  pu_lng: number;
  status: string;
  distance_km: number;
}

interface CoverageHex {
  state: string;
  lga: string;
  contacts: number;
  volunteers: number;
  score: number;
}

interface CanvassTrail {
  volunteer_id: string;
  lat: number;
  lng: number;
  outcome: string;
  time: string;
}

// Nigeria center
const NIGERIA_CENTER = { latitude: 9.0820, longitude: 8.6753 };
const INITIAL_ZOOM = 6;

// Layer visibility
type LayerKey = 'volunteers' | 'rides' | 'coverage' | 'trails' | 'heatmap';

// Role colors
const ROLE_COLORS: Record<string, [number, number, number]> = {
  canvasser: [59, 130, 246],   // blue
  driver: [16, 185, 129],      // green
  caller: [245, 158, 11],      // amber
  coordinator: [139, 92, 246], // purple
  observer: [236, 72, 153],    // pink
};

const STATUS_ARC_COLORS: Record<string, [number, number, number]> = {
  pending: [245, 158, 11],
  matched: [59, 130, 246],
  en_route: [99, 102, 241],
  picked_up: [16, 185, 129],
};

const OUTCOME_COLORS: Record<string, [number, number, number]> = {
  home: [16, 185, 129],
  not_home: [156, 163, 175],
  refused: [239, 68, 68],
  pledged: [59, 130, 246],
  already_voted: [139, 92, 246],
};

export default function GOTVMapPage() {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const deckRef = useRef<Deck | null>(null);

  const [volunteers, setVolunteers] = useState<GeoVolunteer[]>([]);
  const [rides, setRides] = useState<GeoRide[]>([]);
  const [coverage, setCoverage] = useState<CoverageHex[]>([]);
  const [trails, setTrails] = useState<CanvassTrail[]>([]);
  const [selected, setSelected] = useState<GeoVolunteer | null>(null);
  const [layerVisibility, setLayerVisibility] = useState<Record<LayerKey, boolean>>({
    volunteers: true,
    rides: true,
    coverage: false,
    trails: false,
    heatmap: false,
  });
  const [wsConnected, setWsConnected] = useState(false);

  // ─── Data Loading ───────────────────────────────────────────────────────

  const loadGeoData = useCallback(async () => {
    try {
      const [volData, rideData, covData, trailData] = await Promise.all([
        api.getGOTVGeoVolunteers() as Promise<{ volunteers: GeoVolunteer[] }>,
        api.getGOTVGeoRides() as Promise<{ rides: GeoRide[] }>,
        api.getGOTVGeoCoverage() as Promise<{ coverage: CoverageHex[] }>,
        api.getGOTVGeoTrails() as Promise<{ trails: CanvassTrail[] }>,
      ]);
      setVolunteers(volData.volunteers || []);
      setRides(rideData.rides || []);
      setCoverage(covData.coverage || []);
      setTrails(trailData.trails || []);
    } catch {
      // API not available — use empty state
    }
  }, []);

  // ─── WebSocket Connection ───────────────────────────────────────────────

  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsToken = localStorage.getItem('auth_token') || '';
    const wsPartyId = localStorage.getItem('gotv_party_id') || '1';
    const wsHost = import.meta.env.VITE_GOTV_WS_HOST || window.location.host;
    const wsUrl = `${protocol}//${wsHost}/gotv/ws?party_id=${wsPartyId}&token=${wsToken}`;
    let ws: WebSocket | null = null;

    try {
      ws = new WebSocket(wsUrl);
      ws.onopen = () => setWsConnected(true);
      ws.onclose = () => setWsConnected(false);
      ws.onerror = () => setWsConnected(false);
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          if (msg.channel === 'volunteer.location') {
            setVolunteers(prev => prev.map(v =>
              v.id === msg.data.volunteer_id
                ? { ...v, lat: msg.data.latitude, lng: msg.data.longitude }
                : v
            ));
          } else if (msg.channel === 'ride.status') {
            setRides(prev => prev.map(r =>
              r.id === msg.data.ride_id ? { ...r, status: msg.data.status } : r
            ));
          } else if (msg.channel === 'canvass.log') {
            setTrails(prev => [{
              volunteer_id: msg.data.volunteer_id,
              lat: msg.data.latitude,
              lng: msg.data.longitude,
              outcome: msg.data.outcome,
              time: new Date().toISOString(),
            }, ...prev.slice(0, 4999)]);
          }
        } catch { /* ignore parse errors */ }
      };
    } catch { /* WebSocket not available */ }

    return () => { ws?.close(); };
  }, []);

  // ─── Map Initialization ─────────────────────────────────────────────────

  useEffect(() => {
    if (!mapContainer.current || mapRef.current) return;

    const map = new maplibregl.Map({
      container: mapContainer.current,
      style: 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json',
      center: [NIGERIA_CENTER.longitude, NIGERIA_CENTER.latitude],
      zoom: INITIAL_ZOOM,
      pitch: 30,
    });

    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    mapRef.current = map;

    map.on('load', () => {
      loadGeoData();
    });

    return () => { map.remove(); mapRef.current = null; };
  }, [loadGeoData]);

  // ─── Deck.gl Layers ─────────────────────────────────────────────────────

  // H3 hex coverage
  const h3Data = useMemo(() => {
    // Convert coverage data to H3 hexes (resolution 5 for state/LGA overview)
    // Since we have state/lga aggregates, place a hex at Nigeria's center for each
    const nigeriaStates: Record<string, [number, number]> = {
      'FCT': [9.0579, 7.4951], 'LA': [6.5244, 3.3792], 'KN': [12.0022, 8.5919],
      'RV': [4.8156, 7.0498], 'OG': [7.1475, 3.3619], 'KD': [10.5105, 7.4165],
      'OY': [7.3775, 3.9470], 'AN': [6.2209, 7.0672], 'EN': [6.4584, 7.5464],
      'AB': [5.4527, 7.5248], 'IM': [5.4921, 7.0260], 'DE': [6.2000, 6.7000],
      'ED': [6.3350, 5.6037], 'EB': [5.9631, 8.0700], 'CR': [5.9631, 8.3300],
      'AK': [5.0353, 7.9128], 'BA': [5.0000, 8.0000], 'BE': [7.2500, 5.2000],
    };
    return coverage.map(c => {
      const coords = nigeriaStates[c.state] || [NIGERIA_CENTER.latitude, NIGERIA_CENTER.longitude];
      const h3Index = latLngToCell(coords[0], coords[1], 4);
      return { ...c, h3Index };
    });
  }, [coverage]);

  const layers = useMemo(() => {
    const result: (ScatterplotLayer | ArcLayer | HeatmapLayer | H3HexagonLayer)[] = [];

    // Volunteer scatter (live positions)
    if (layerVisibility.volunteers) {
      result.push(new ScatterplotLayer({
        id: 'volunteers',
        data: volunteers.filter(v => v.lat !== 0 && v.lng !== 0),
        getPosition: (d: GeoVolunteer) => [d.lng, d.lat],
        getFillColor: (d: GeoVolunteer) => [
          ...(ROLE_COLORS[d.role] || [59, 130, 246]),
          d.active ? 220 : 100,
        ] as [number, number, number, number],
        getRadius: (d: GeoVolunteer) => d.has_vehicle ? 800 : 500,
        radiusMinPixels: 4,
        radiusMaxPixels: 20,
        pickable: true,
        onClick: (info: { object?: GeoVolunteer }) => { if (info.object) setSelected(info.object); },
      }));
    }

    // Ride arcs (pickup → polling unit)
    if (layerVisibility.rides) {
      result.push(new ArcLayer({
        id: 'rides',
        data: rides.filter(r => r.pickup_lat !== 0 && r.pu_lat !== 0),
        getSourcePosition: (d: GeoRide) => [d.pickup_lng, d.pickup_lat],
        getTargetPosition: (d: GeoRide) => [d.pu_lng, d.pu_lat],
        getSourceColor: (d: GeoRide) => STATUS_ARC_COLORS[d.status] || [156, 163, 175],
        getTargetColor: () => [16, 185, 129] as [number, number, number],
        getWidth: 2,
        pickable: true,
      }));
    }

    // H3 hex coverage
    if (layerVisibility.coverage && h3Data.length > 0) {
      result.push(new H3HexagonLayer({
        id: 'coverage',
        data: h3Data,
        getHexagon: (d: typeof h3Data[0]) => d.h3Index,
        getFillColor: (d: typeof h3Data[0]) => {
          const s = d.score;
          if (s > 0.7) return [16, 185, 129, 140];
          if (s > 0.3) return [245, 158, 11, 140];
          return [239, 68, 68, 140];
        },
        getElevation: (d: typeof h3Data[0]) => d.contacts,
        elevationScale: 10,
        extruded: true,
        pickable: true,
      }));
    }

    // Canvass trail heatmap
    if (layerVisibility.trails) {
      result.push(new ScatterplotLayer({
        id: 'trails',
        data: trails,
        getPosition: (d: CanvassTrail) => [d.lng, d.lat],
        getFillColor: (d: CanvassTrail) => [...(OUTCOME_COLORS[d.outcome] || [156, 163, 175]), 180] as [number, number, number, number],
        getRadius: 300,
        radiusMinPixels: 3,
        radiusMaxPixels: 10,
        pickable: true,
      }));
    }

    // Turnout heatmap
    if (layerVisibility.heatmap) {
      result.push(new HeatmapLayer({
        id: 'heatmap',
        data: trails,
        getPosition: (d: CanvassTrail) => [d.lng, d.lat],
        getWeight: () => 1,
        radiusPixels: 40,
        intensity: 1.5,
        threshold: 0.1,
      }));
    }

    return result;
  }, [volunteers, rides, h3Data, trails, layerVisibility]);

  // Update deck.gl overlay when layers change
  useEffect(() => {
    if (!mapRef.current) return;

    if (deckRef.current) {
      deckRef.current.setProps({ layers });
    } else {
      const map = mapRef.current;
      const deck = new Deck({
        parent: map.getCanvasContainer() as HTMLDivElement,
        controller: false,
        layers,
        style: { position: 'absolute', top: '0', left: '0', pointerEvents: 'none' },
        viewState: {
          longitude: map.getCenter().lng,
          latitude: map.getCenter().lat,
          zoom: map.getZoom(),
          pitch: map.getPitch(),
          bearing: map.getBearing(),
        },
      });

      const syncDeck = () => {
        deck.setProps({
          viewState: {
            longitude: map.getCenter().lng,
            latitude: map.getCenter().lat,
            zoom: map.getZoom(),
            pitch: map.getPitch(),
            bearing: map.getBearing(),
          },
        });
      };

      map.on('move', syncDeck);
      map.on('zoom', syncDeck);
      map.on('pitch', syncDeck);
      map.on('rotate', syncDeck);
      deckRef.current = deck;
    }
  }, [layers]);

  // ─── Layer Toggle ───────────────────────────────────────────────────────

  const toggleLayer = (key: LayerKey) => {
    setLayerVisibility(prev => ({ ...prev, [key]: !prev[key] }));
  };

  // ─── Stats ──────────────────────────────────────────────────────────────

  const activeVols = volunteers.filter(v => v.active).length;
  const activeRides = rides.filter(r => r.status === 'en_route' || r.status === 'picked_up').length;
  const pendingRides = rides.filter(r => r.status === 'pending').length;
  const totalKnocks = trails.length;

  // ─── Render ─────────────────────────────────────────────────────────────

  return (
    <div className="relative w-full" style={{ height: 'calc(100vh - 200px)', minHeight: '500px' }}>
      {/* Map container */}
      <div ref={mapContainer} className="absolute inset-0 rounded-lg overflow-hidden" />

      {/* Controls overlay */}
      <div className="absolute top-4 left-4 z-10 space-y-2">
        {/* Connection status */}
        <div className="flex items-center gap-2 bg-background/90 backdrop-blur-sm rounded-lg px-3 py-2 shadow-sm">
          <Radio className={`h-4 w-4 ${wsConnected ? 'text-green-500' : 'text-red-500'}`} />
          <span className="text-xs">{wsConnected ? 'Live' : 'Offline'}</span>
        </div>

        {/* Stats cards */}
        <Card className="bg-background/90 backdrop-blur-sm shadow-sm">
          <CardContent className="p-3 space-y-1">
            <div className="flex items-center gap-2 text-sm">
              <MapPin className="h-4 w-4 text-blue-500" />
              <span>{activeVols} active volunteers</span>
            </div>
            <div className="flex items-center gap-2 text-sm">
              <Car className="h-4 w-4 text-green-500" />
              <span>{activeRides} rides in progress, {pendingRides} pending</span>
            </div>
            <div className="flex items-center gap-2 text-sm">
              <Navigation className="h-4 w-4 text-purple-500" />
              <span>{totalKnocks} door knocks</span>
            </div>
          </CardContent>
        </Card>

        {/* Layer toggles */}
        <Card className="bg-background/90 backdrop-blur-sm shadow-sm">
          <CardContent className="p-3 space-y-2">
            <div className="text-xs font-semibold text-muted-foreground uppercase">Layers</div>
            {([
              { key: 'volunteers' as LayerKey, label: 'Volunteers', color: 'bg-blue-500' },
              { key: 'rides' as LayerKey, label: 'Ride Arcs', color: 'bg-green-500' },
              { key: 'coverage' as LayerKey, label: 'H3 Coverage', color: 'bg-orange-500' },
              { key: 'trails' as LayerKey, label: 'Canvass Trails', color: 'bg-purple-500' },
              { key: 'heatmap' as LayerKey, label: 'Turnout Heat', color: 'bg-red-500' },
            ]).map(layer => (
              <button
                key={layer.key}
                onClick={() => toggleLayer(layer.key)}
                className="flex items-center gap-2 w-full text-sm hover:bg-accent rounded px-1 py-0.5"
              >
                {layerVisibility[layer.key]
                  ? <Eye className="h-3 w-3" />
                  : <EyeOff className="h-3 w-3 text-muted-foreground" />
                }
                <span className={`h-2 w-2 rounded-full ${layer.color}`} />
                <span className={layerVisibility[layer.key] ? '' : 'text-muted-foreground'}>{layer.label}</span>
              </button>
            ))}
          </CardContent>
        </Card>
      </div>

      {/* Selected volunteer detail */}
      {selected && (
        <div className="absolute bottom-4 left-4 z-10">
          <Card className="bg-background/95 backdrop-blur-sm shadow-lg max-w-sm">
            <CardContent className="p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="font-semibold">{selected.name}</span>
                <Badge variant={selected.active ? 'default' : 'secondary'}>{selected.role}</Badge>
              </div>
              <div className="grid grid-cols-3 gap-2 text-sm text-muted-foreground">
                <div>Doors: {selected.doors_knocked}</div>
                <div>Calls: {selected.calls_made}</div>
                <div>Rides: {selected.rides_given}</div>
              </div>
              {selected.has_vehicle && (
                <div className="text-xs text-green-600 mt-1">🚗 Vehicle ({selected.vehicle_capacity} seats)</div>
              )}
              <Button size="sm" variant="ghost" className="mt-2" onClick={() => setSelected(null)}>
                Close
              </Button>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Legend */}
      <div className="absolute bottom-4 right-4 z-10">
        <Card className="bg-background/90 backdrop-blur-sm shadow-sm">
          <CardContent className="p-3">
            <div className="text-xs font-semibold text-muted-foreground uppercase mb-2">Volunteer Roles</div>
            <div className="space-y-1">
              {Object.entries(ROLE_COLORS).map(([role, color]) => (
                <div key={role} className="flex items-center gap-2 text-xs">
                  <span className="h-3 w-3 rounded-full" style={{ backgroundColor: `rgb(${color.join(',')})` }} />
                  <span className="capitalize">{role}</span>
                </div>
              ))}
            </div>
            <div className="text-xs font-semibold text-muted-foreground uppercase mt-3 mb-2">Ride Status</div>
            <div className="space-y-1">
              {Object.entries(STATUS_ARC_COLORS).map(([status, color]) => (
                <div key={status} className="flex items-center gap-2 text-xs">
                  <span className="h-3 w-3 rounded-full" style={{ backgroundColor: `rgb(${color.join(',')})` }} />
                  <span className="capitalize">{status.replace('_', ' ')}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Refresh */}
      <div className="absolute top-4 right-4 z-10">
        <Button size="sm" variant="secondary" className="shadow-sm" onClick={loadGeoData}>
          Refresh Data
        </Button>
      </div>
    </div>
  );
}
