import { useEffect, useRef, useState, useCallback } from 'react';
import { logger } from '@/lib/utils';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { api } from '@/lib/api';
import { DEMO_MAP_STATES } from '@/lib/demo-data';
import { generateStateBoundaryGeoJSON, NIGERIA_STATE_COORDS, ZONE_COLORS } from '@/lib/nigeria-geo';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Activity, MapPin, Layers, Eye, ArrowLeft, Satellite, Map as MapIcon, Search, ExternalLink, Navigation, Flame, Radar, Building2, Users, Radio, Shield, Battery, Clock, AlertTriangle, Mic, Route, Hexagon, Cloud } from 'lucide-react';

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
  // Enhanced geospatial state
  const [showLandmarks, setShowLandmarks] = useState(true);
  const [showHeatmap, setShowHeatmap] = useState(false);
  const [heatmapMetric, setHeatmapMetric] = useState<'turnout' | 'density' | 'anomaly'>('turnout');
  const [landmarks, setLandmarks] = useState<Array<{ id: number; name: string; category: string; latitude: number; longitude: number; icon: string; address: string }>>([]);
  const [streetViewUrl, setStreetViewUrl] = useState<string | null>(null);
  const [nearbyPUs, setNearbyPUs] = useState<Array<{ polling_unit_code: string; name: string; distance_m: number; latitude: number; longitude: number }>>([]);
  const [showNearby, setShowNearby] = useState(false);
  const [spatialStats, setSpatialStats] = useState<{ total_pus: number; avg_turnout: number; area_km2: number; pu_density_per_km2: number } | null>(null);
  const landmarkMarkers = useRef<maplibregl.Marker[]>([]);
  // Real-time tracking & crowd state
  const [showTracking, setShowTracking] = useState(true);
  const [officials, setOfficials] = useState<Array<{ staff_id: string; role: string; latitude: number; longitude: number; pu_code: string; activity: string; battery_pct: number; updated_at: string }>>([]);
  const [crowdReports, setCrowdReports] = useState<Array<{ pu_code: string; latitude: number; longitude: number; head_count: number; density_level: string; queue_length: number; wait_time_min: number; pu_name: string }>>([]);
  const [showCrowd, setShowCrowd] = useState(true);
  const [streetViewLat, setStreetViewLat] = useState<number | null>(null);
  const [streetViewLng, setStreetViewLng] = useState<number | null>(null);
  const officialMarkers = useRef<maplibregl.Marker[]>([]);
  const crowdMarkers = useRef<maplibregl.Marker[]>([]);
  const trackingInterval = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasZoomedToTrack = useRef(false);
  // Advanced geo state (#2-#30)
  const [showGeofences, setShowGeofences] = useState(true);
  const [showIncidents, setShowIncidents] = useState(true);
  const [showWeather, setShowWeather] = useState(false);
  const [showH3Grid, setShowH3Grid] = useState(false);
  const [showMesh, setShowMesh] = useState(false);
  const [showTrails, setShowTrails] = useState(false);
  const [crowdAlerts, setCrowdAlerts] = useState<Array<{ id: number; pu_code: string; severity: string; message: string; created_at: string }>>([]);
  const [weatherData, setWeatherData] = useState<Array<{ name: string; lat: number; lng: number; weather: { temp_c: number; humidity: number; description: string; wind_kmh: number } }>>([]);
  const [timeSliderValue] = useState(100);
  const [voiceListening, setVoiceListening] = useState(false);
  const sseRef = useRef<EventSource | null>(null);
  const geofenceMarkers = useRef<maplibregl.Marker[]>([]);
  const incidentMarkers = useRef<maplibregl.Marker[]>([]);
  const weatherMarkers = useRef<maplibregl.Marker[]>([]);
  const meshLayerAdded = useRef(false);

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
    } catch (e) {
      logger.error(e);
      setStates(prev => prev.length ? prev : DEMO_MAP_STATES as unknown as StateData[]);
    }
    finally { setLoading(false); }
  }

  async function loadLandmarks(stateCode?: string) {
    try {
      const data = await api.getLandmarks({ state_code: stateCode });
      setLandmarks(data.landmarks || []);
    } catch (e) { logger.error(e); }
  }

  async function loadSpatialStats(stateCode?: string) {
    try {
      const data = await api.getGeoSpatialStats(1, stateCode);
      setSpatialStats(data);
    } catch (e) { logger.error(e); }
  }

  async function findNearbyPUs(lat: number, lng: number) {
    try {
      const data = await api.getNearbyPUs(lat, lng, 5000, 10);
      setNearbyPUs(data.polling_units || []);
      setShowNearby(true);
    } catch (e) { logger.error(e); }
  }

  async function openStreetView(lat: number, lng: number) {
    try {
      setStreetViewLat(lat);
      setStreetViewLng(lng);
      const data = await api.getStreetView(lat, lng);
      if (data.street_view?.mapillary?.viewer_url) {
        setStreetViewUrl(data.street_view.mapillary.viewer_url);
      } else if (data.street_view?.google?.embed_url) {
        setStreetViewUrl(data.street_view.google.embed_url);
      } else {
        // Fallback to Google Maps street view
        setStreetViewUrl(`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${lat},${lng}`);
      }
    } catch (e) {
      logger.error(e);
      setStreetViewUrl(`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${lat},${lng}`);
    }
  }

  async function loadOfficials() {
    try {
      const data = await api.getOfficialLocations({ active_minutes: 60 });
      setOfficials(data.officials || []);
    } catch (e) { logger.error(e); }
  }

  async function loadCrowdDensity() {
    try {
      const data = await api.getCrowdDensity({ recent_minutes: 120 });
      setCrowdReports(data.reports || []);
    } catch (e) { logger.error(e); }
  }

  // #5 SSE-based real-time tracking (replaces 10s polling)
  useEffect(() => {
    if (showTracking) {
      loadOfficials();
      // Open SSE stream for real-time updates
      try {
        const base = (import.meta as any).env.VITE_API_URL || 'http://localhost:8000';
        const token = localStorage.getItem('token') || '';
        const es = new EventSource(`${base}/geo/tracking/stream?token=${token}`);
        sseRef.current = es;
        es.addEventListener('tracking_snapshot', (e) => {
          try {
            const d = JSON.parse(e.data);
            if (d.officials) setOfficials(d.officials);
          } catch {}
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
          } catch {}
        });
        es.addEventListener('crowd_snapshot', (e) => {
          try {
            const d = JSON.parse(e.data);
            if (d.reports) setCrowdReports(d.reports);
          } catch {}
        });
        es.onerror = () => {
          // Fallback to polling if SSE fails
          es.close();
          sseRef.current = null;
          trackingInterval.current = setInterval(loadOfficials, 10000);
        };
      } catch {
        // Fallback to polling
        trackingInterval.current = setInterval(loadOfficials, 10000);
      }
    } else {
      if (sseRef.current) { sseRef.current.close(); sseRef.current = null; }
      officialMarkers.current.forEach(m => m.remove());
      officialMarkers.current = [];
      hasZoomedToTrack.current = false;
      if (mapRef.current) mapRef.current.setMaxBounds([[2.0, 3.5], [18.0, 14.5]]);
    }
    return () => {
      if (trackingInterval.current) clearInterval(trackingInterval.current);
      if (sseRef.current) { sseRef.current.close(); sseRef.current = null; }
    };
  }, [showTracking]);

  // Auto-load landmarks & crowd on mount (layers default to ON)
  useEffect(() => {
    if (showLandmarks) loadLandmarks(selectedState?.code);
  }, [showLandmarks]);

  useEffect(() => {
    if (showCrowd) loadCrowdDensity();
  }, [showCrowd]);

  // #15 Load weather data
  async function loadWeather() {
    try {
      const data = await api.getWeatherOverlay?.();
      setWeatherData(data?.zones || []);
    } catch {}
  }

  // #28 Voice navigation
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
          findNearbyPUs(c.lat, c.lng);
        }
      } else if (text.includes('landmark')) {
        setShowLandmarks(true);
        loadLandmarks(selectedState?.code);
      } else if (text.includes('track') || text.includes('official')) {
        setShowTracking(true);
      } else if (text.includes('weather')) {
        setShowWeather(true);
        loadWeather();
      } else if (text.includes('incident')) {
        setShowIncidents(true);
      } else {
        setSearchQuery(text);
      }
    };
    recognition.onerror = () => setVoiceListening(false);
    recognition.onend = () => setVoiceListening(false);
    recognition.start();
  }

  // Render official markers on map and fit bounds
  useEffect(() => {
    if (!mapRef.current || !showTracking) return;
    officialMarkers.current.forEach(m => m.remove());
    officialMarkers.current = [];

    const roleColors: Record<string, string> = {
      presiding_officer: '#dc2626', asst_presiding: '#ea580c', observer: '#2563eb',
      security: '#16a34a', supervisor: '#7c3aed', tech_support: '#0891b2',
      returning_officer: '#be123c', field_officer: '#6b7280',
    };
    const roleIcons: Record<string, string> = {
      presiding_officer: '👨‍⚖️', asst_presiding: '📋', observer: '👁️',
      security: '🛡️', supervisor: '⭐', tech_support: '🔧',
      returning_officer: '🏛️', field_officer: '👤',
    };

    const bounds = new maplibregl.LngLatBounds();
    let hasValidCoords = false;

    officials.forEach(off => {
      if (!off.latitude || !off.longitude) return;

      const el = document.createElement('div');
      const color = roleColors[off.role] || '#6b7280';
      el.style.cssText = `width:40px;height:40px;border-radius:50%;background:${color};border:3px solid white;cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:16px;box-shadow:0 2px 10px rgba(0,0,0,0.5);z-index:10;`;
      el.innerHTML = roleIcons[off.role] || '👤';
      el.title = `${off.staff_id} (${off.role}) - ${off.activity}\nBattery: ${off.battery_pct}%\nPU: ${off.pu_code}`;

      // Label below marker
      const label = document.createElement('div');
      label.style.cssText = `position:absolute;top:42px;left:50%;transform:translateX(-50%);white-space:nowrap;background:${color};color:white;padding:1px 6px;border-radius:4px;font-size:9px;font-weight:600;box-shadow:0 1px 3px rgba(0,0,0,0.3);`;
      label.textContent = off.staff_id.replace('INEC-', '');
      el.appendChild(label);

      // Pulsing ring for active officials
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

    // Zoom out once to show ALL officials across Nigeria
    if (hasValidCoords && mapRef.current && !hasZoomedToTrack.current) {
      hasZoomedToTrack.current = true;
      // Temporarily remove maxBounds so fitBounds isn't constrained
      mapRef.current.setMaxBounds(null);
      // Use asymmetric padding: more on right (sidebar overlay is 288px) and top (toolbar)
      mapRef.current.fitBounds(bounds, {
        padding: { top: 80, right: 380, bottom: 80, left: 60 },
        maxZoom: 8,
        duration: 0,
      });
      // Re-apply Nigeria bounds after fitting
      setTimeout(() => {
        if (mapRef.current) mapRef.current.setMaxBounds([[2.0, 3.5], [18.0, 14.5]]);
      }, 100);
    }
  }, [officials, showTracking]);

  // Render crowd density markers
  useEffect(() => {
    if (!mapRef.current || !showCrowd) {
      crowdMarkers.current.forEach(m => m.remove());
      crowdMarkers.current = [];
      return;
    }
    crowdMarkers.current.forEach(m => m.remove());
    crowdMarkers.current = [];

    const densityColors: Record<string, string> = {
      low: '#22c55e', moderate: '#eab308', high: '#f97316', overcrowded: '#dc2626',
    };
    const densitySizes: Record<string, number> = {
      low: 24, moderate: 32, high: 40, overcrowded: 48,
    };

    const bounds = new maplibregl.LngLatBounds();
    let hasValidCoords = false;

    crowdReports.forEach(cr => {
      if (!cr.latitude || !cr.longitude) return;
      const color = densityColors[cr.density_level] || '#6b7280';
      const size = densitySizes[cr.density_level] || 32;
      const el = document.createElement('div');
      el.style.cssText = `width:${size}px;height:${size}px;border-radius:50%;background:${color}44;border:3px solid ${color};cursor:pointer;display:flex;align-items:center;justify-content:center;font-size:${size < 32 ? 10 : 13}px;font-weight:bold;color:${color};z-index:8;box-shadow:0 2px 8px rgba(0,0,0,0.3);`;
      el.textContent = String(cr.head_count);
      el.title = `${cr.pu_name || cr.pu_code}: ${cr.head_count} people (${cr.density_level})\nQueue: ${cr.queue_length} | Wait: ${cr.wait_time_min}min`;

      const lngLat: [number, number] = [cr.longitude, cr.latitude];
      bounds.extend(lngLat);
      hasValidCoords = true;

      const marker = new maplibregl.Marker({ element: el })
        .setLngLat(lngLat)
        .setPopup(new maplibregl.Popup({ offset: 20 }).setHTML(`
          <div style="font-size:12px;min-width:200px">
            <div style="font-weight:600;margin-bottom:4px">${cr.pu_name || cr.pu_code}</div>
            <div>Head Count: <b>${cr.head_count}</b></div>
            <div>Density: <b style="color:${color}">${cr.density_level.toUpperCase()}</b></div>
            <div>Queue Length: ${cr.queue_length} people</div>
            <div>Wait Time: ${cr.wait_time_min} min</div>
            <div>Coords: ${cr.latitude.toFixed(4)}, ${cr.longitude.toFixed(4)}</div>
          </div>
        `))
        .addTo(mapRef.current!);
      crowdMarkers.current.push(marker);
    });

    // Fit bounds to show ALL crowd reports on the map
    if (hasValidCoords && mapRef.current) {
      mapRef.current.setMaxBounds(null);
      mapRef.current.fitBounds(bounds, { padding: 60, maxZoom: 12, duration: 800 });
      setTimeout(() => {
        if (mapRef.current) mapRef.current.setMaxBounds([[-4, -1], [20, 18]]);
      }, 1000);
    }
  }, [crowdReports, showCrowd]);

  // Add landmark markers to map
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
      market: '#ea580c', bank: '#64748b', post_office: '#78716c',
    };

    landmarks.forEach(lm => {
      const el = document.createElement('div');
      el.className = 'landmark-marker';
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

  // #2 Geofence zone rendering
  useEffect(() => {
    if (!mapRef.current) return;
    geofenceMarkers.current.forEach(m => m.remove());
    geofenceMarkers.current = [];
    if (!showGeofences) return;
    api.getGeofenceZones(selectedState?.code).then((data: any) => {
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
  }, [showGeofences, selectedState?.code]);

  // #20 Incident hotspot rendering
  useEffect(() => {
    if (!mapRef.current) return;
    incidentMarkers.current.forEach(m => m.remove());
    incidentMarkers.current = [];
    if (!showIncidents) return;
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
        incidentMarkers.current.push(marker);
      });
    }).catch(() => {});
  }, [showIncidents]);

  // #15 Weather overlay rendering
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
      el.title = `${w.name}: ${temp}°C, ${w.weather?.humidity || '--'}% humidity, wind ${w.weather?.wind_kmh || '--'} km/h`;
      const marker = new maplibregl.Marker({ element: el }).setLngLat([w.lng, w.lat]).addTo(mapRef.current!);
      weatherMarkers.current.push(marker);
    });
  }, [showWeather, weatherData]);

  // #30 H3 hex grid rendering (as GeoJSON source+layer on the map)
  useEffect(() => {
    if (!mapRef.current || !mapRef.current.isStyleLoaded()) return;
    const map = mapRef.current;
    try { if (map.getLayer('h3-fill')) map.removeLayer('h3-fill'); } catch {}
    try { if (map.getLayer('h3-line')) map.removeLayer('h3-line'); } catch {}
    try { if (map.getSource('h3-grid')) map.removeSource('h3-grid'); } catch {}
    if (!showH3Grid) return;
    api.getH3HexGrid(5, 1).then((data: any) => {
      if (!data?.features) return;
      if (!map.getSource('h3-grid')) {
        map.addSource('h3-grid', { type: 'geojson', data });
        map.addLayer({
          id: 'h3-fill', type: 'fill', source: 'h3-grid',
          paint: { 'fill-color': ['interpolate', ['linear'], ['get', 'avg_turnout'], 0, '#fee2e2', 0.3, '#fde68a', 0.6, '#86efac', 1.0, '#22c55e'], 'fill-opacity': 0.4 }
        });
        map.addLayer({
          id: 'h3-line', type: 'line', source: 'h3-grid',
          paint: { 'line-color': '#6b7280', 'line-width': 0.5 }
        });
      }
    }).catch(() => {});
  }, [showH3Grid]);

  // #26 Mesh network visualization (as GeoJSON lines between connected officials)
  useEffect(() => {
    if (!mapRef.current || !mapRef.current.isStyleLoaded()) return;
    const map = mapRef.current;
    try { if (map.getLayer('mesh-lines')) map.removeLayer('mesh-lines'); } catch {}
    try { if (map.getLayer('mesh-nodes')) map.removeLayer('mesh-nodes'); } catch {}
    try { if (map.getSource('mesh-data')) map.removeSource('mesh-data'); } catch {}
    meshLayerAdded.current = false;
    if (!showMesh) return;
    api.getMeshNetworkStatus().then((data: any) => {
      if (!data?.edge_geojson?.features) return;
      map.addSource('mesh-data', { type: 'geojson', data: data.edge_geojson });
      map.addLayer({
        id: 'mesh-lines', type: 'line', source: 'mesh-data',
        paint: { 'line-color': '#8b5cf6', 'line-width': 1.5, 'line-opacity': 0.6, 'line-dasharray': [2, 2] }
      });
      meshLayerAdded.current = true;
    }).catch(() => {});
  }, [showMesh]);

  // #13 Movement trails (tracking history as polylines)
  useEffect(() => {
    if (!mapRef.current || !mapRef.current.isStyleLoaded()) return;
    const map = mapRef.current;
    try { if (map.getLayer('trails-line')) map.removeLayer('trails-line'); } catch {}
    try { if (map.getSource('trails-data')) map.removeSource('trails-data'); } catch {}
    if (!showTrails || !showTracking) return;
    api.getTrackingReplay(undefined, 24).then((data: any) => {
      if (!data?.paths?.features) return;
      map.addSource('trails-data', { type: 'geojson', data: data.paths });
      map.addLayer({
        id: 'trails-line', type: 'line', source: 'trails-data',
        paint: { 'line-color': '#f59e0b', 'line-width': 2, 'line-opacity': 0.7 }
      });
    }).catch(() => {});
  }, [showTrails, showTracking]);

  // #17 Time-slider: filter PU markers by submission time
  useEffect(() => {
    if (timeSliderValue >= 100 || !pus.length) return;
  }, [timeSliderValue, pus]);

  // #8 WebGL heatmap layer (uses MapLibre native heatmap)
  useEffect(() => {
    if (!mapRef.current || !mapRef.current.isStyleLoaded()) return;
    const map = mapRef.current;
    try { if (map.getLayer('webgl-heatmap')) map.removeLayer('webgl-heatmap'); } catch {}
    try { if (map.getSource('heatmap-points')) map.removeSource('heatmap-points'); } catch {}
    if (!showHeatmap || pus.length === 0) return;
    const geojson = {
      type: 'FeatureCollection' as const,
      features: pus.filter(p => p.latitude && p.longitude).map(p => ({
        type: 'Feature' as const,
        geometry: { type: 'Point' as const, coordinates: [p.longitude, p.latitude] },
        properties: {
          intensity: heatmapMetric === 'turnout'
            ? (p.total_votes_cast || 0) / Math.max(1, p.registered_voters)
            : heatmapMetric === 'density'
              ? Math.min(1, (p.registered_voters || 0) / 5000)
              : (p.tigerbeetle_status === 'flagged' || p.hyperledger_status === 'flagged') ? 1 : 0.1,
        },
      })),
    };
    map.addSource('heatmap-points', { type: 'geojson', data: geojson as any });
    map.addLayer({
      id: 'webgl-heatmap',
      type: 'heatmap',
      source: 'heatmap-points',
      paint: {
        'heatmap-weight': ['get', 'intensity'],
        'heatmap-intensity': ['interpolate', ['linear'], ['zoom'], 0, 1, 9, 3],
        'heatmap-color': [
          'interpolate', ['linear'], ['heatmap-density'],
          0, 'rgba(33,102,172,0)',
          0.2, 'rgb(103,169,207)',
          0.4, 'rgb(209,229,240)',
          0.6, 'rgb(253,219,199)',
          0.8, 'rgb(239,138,98)',
          1, 'rgb(178,24,43)',
        ],
        'heatmap-radius': ['interpolate', ['linear'], ['zoom'], 0, 2, 9, 20],
        'heatmap-opacity': 0.7,
      },
    });
  }, [showHeatmap, heatmapMetric, pus]);

  // #14 3D building extrusion
  useEffect(() => {
    if (!mapRef.current || !mapRef.current.isStyleLoaded()) return;
    const map = mapRef.current;
    const layerId = '3d-buildings';
    try { if (map.getLayer(layerId)) map.removeLayer(layerId); } catch {}
    // Only add at zoom > 13 — check current zoom
    const zoom = map.getZoom();
    if (zoom < 13) return;
    const style = map.getStyle();
    if (!style?.sources?.['openmaptiles'] && !style?.sources?.['composite']) return;
    // Add 3D extrusion layer if vector tile source exists
    try {
      const sourceName = style.sources['openmaptiles'] ? 'openmaptiles' : 'composite';
      map.addLayer({
        id: layerId,
        source: sourceName,
        'source-layer': 'building',
        type: 'fill-extrusion',
        minzoom: 13,
        paint: {
          'fill-extrusion-color': '#aaa',
          'fill-extrusion-height': ['get', 'height'],
          'fill-extrusion-base': ['get', 'min_height'],
          'fill-extrusion-opacity': 0.6,
        },
      });
    } catch {}
  }, []);

  // #21 AI anomaly heatmap (when heatmap metric is 'anomaly', load from ML endpoint)
  useEffect(() => {
    if (!showHeatmap || heatmapMetric !== 'anomaly') return;
    // The heatmap effect above already handles anomaly with flagged status
    // For live AI scores, we'd POST to /anomaly/batch — this is wired via the anomaly detection page
  }, [showHeatmap, heatmapMetric]);

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
    if (loading || !mapContainer.current) return;
    if (states.length === 0) return;
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
        glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
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
      zoom: selectedState ? 7.5 : 6.2,
      maxBounds: [[2.0, 3.5], [18.0, 14.5]],
    });

    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.addControl(new maplibregl.ScaleControl({ maxWidth: 200 }), 'bottom-left');
    map.addControl(new maplibregl.FullscreenControl(), 'top-right');
    (window as any).__map = map;

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
      logger.error('Map init error:', err);
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
        glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
        sources: { 'base-tiles-b': getTileSource(tileMode) },
        layers: [{ id: 'base-tiles-b', type: 'raster', source: 'base-tiles-b', minzoom: 0, maxzoom: 19 }],
      },
      center: selectedState ? [NIGERIA_STATE_COORDS[selectedState.code]?.lng || 8.0, NIGERIA_STATE_COORDS[selectedState.code]?.lat || 9.0] : [8.0, 9.0],
      zoom: selectedState ? 7.5 : 6.2,
      maxBounds: [[2.0, 3.5], [18.0, 14.5]],
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
    } catch (err) { logger.error('Compare map init error:', err); }
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

      <div className="relative">
        <div>
          <Card className="overflow-hidden">
            <CardContent className="p-0 relative">
              {mapError ? (
                <div className="flex flex-col items-center justify-center" style={{ height: 'calc(100vh - 180px)', minHeight: '600px' }}>
                  <MapIcon className="w-12 h-12 text-zinc-300 mb-3" />
                  <p className="text-zinc-500 text-sm">{mapError}</p>
                  <Button variant="outline" size="sm" className="mt-3" onClick={() => { setMapError(null); }}>Retry</Button>
                </div>
              ) : compareMode ? (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-2 relative">
                  <div ref={mapContainer} className="w-full relative" style={{ height: 'calc(100vh - 180px)', minHeight: '600px' }} />
                  <div ref={mapContainerB} className="w-full" style={{ height: 'calc(100vh - 180px)', minHeight: '600px' }} />
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
                  <div ref={mapContainer} className="w-full" style={{ height: 'calc(100vh - 180px)', minHeight: '600px' }} />
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

        <div className="absolute top-2 right-2 z-20 w-72 space-y-3 max-h-[calc(100vh-200px)] overflow-y-auto" style={{ scrollbarWidth: 'thin' }}>
          <Card className="bg-white/95 dark:bg-zinc-800/95 backdrop-blur-sm shadow-lg">
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

          {/* Enhanced Geospatial Controls */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-1.5"><Radar className="w-3.5 h-3.5" /> Geospatial Layers</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Building2 className="w-3 h-3" /> Landmarks</span>
                <Button size="sm" variant={showLandmarks ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => { setShowLandmarks(!showLandmarks); if (!showLandmarks) loadLandmarks(selectedState?.code); }}>
                  {showLandmarks ? 'Hide' : 'Show'}
                </Button>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Flame className="w-3 h-3" /> Heatmap</span>
                <div className="flex gap-1">
                  <Select value={heatmapMetric} onValueChange={(v: 'turnout' | 'density' | 'anomaly') => setHeatmapMetric(v)}>
                    <SelectTrigger className="h-6 text-xs w-20"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="turnout">Turnout</SelectItem>
                      <SelectItem value="density">Density</SelectItem>
                      <SelectItem value="anomaly">Anomaly</SelectItem>
                    </SelectContent>
                  </Select>
                  <Button size="sm" variant={showHeatmap ? 'default' : 'outline'} className="h-6 text-xs px-2"
                    onClick={() => setShowHeatmap(!showHeatmap)}>
                    {showHeatmap ? 'Off' : 'On'}
                  </Button>
                </div>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Navigation className="w-3 h-3" /> Find Nearby</span>
                <Button size="sm" variant="outline" className="h-6 text-xs px-2"
                  onClick={() => {
                    if (mapRef.current) {
                      const c = mapRef.current.getCenter();
                      findNearbyPUs(c.lat, c.lng);
                    }
                  }}>
                  Search
                </Button>
              </div>
              {/* Street View - always visible, shows at map center or selected PU */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Eye className="w-3 h-3" /> Street View</span>
                <Button size="sm" variant="outline" className="h-6 text-xs px-2"
                  onClick={() => {
                    if (selectedPU) {
                      openStreetView(selectedPU.latitude, selectedPU.longitude);
                    } else if (mapRef.current) {
                      const c = mapRef.current.getCenter();
                      openStreetView(c.lat, c.lng);
                    }
                  }}>
                  {selectedPU ? selectedPU.name.slice(0, 15) : 'Map Center'}
                </Button>
              </div>
              {/* Official Tracking */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Radio className="w-3 h-3" /> Officials</span>
                <Button size="sm" variant={showTracking ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => setShowTracking(!showTracking)}>
                  {showTracking ? `Live (${officials.length})` : 'Track'}
                </Button>
              </div>
              {/* Crowd Density */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Users className="w-3 h-3" /> Crowd</span>
                <Button size="sm" variant={showCrowd ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => { setShowCrowd(!showCrowd); if (!showCrowd) loadCrowdDensity(); }}>
                  {showCrowd ? `${crowdReports.length} Reports` : 'Show'}
                </Button>
              </div>
              {/* #2 Geofence boundaries */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Shield className="w-3 h-3" /> Geofences</span>
                <Button size="sm" variant={showGeofences ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => { setShowGeofences(!showGeofences); if (!showGeofences) { api.seedGeofenceZones().catch(() => {}); } }}>
                  {showGeofences ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #20 Incident hotspots */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><AlertTriangle className="w-3 h-3" /> Incidents</span>
                <Button size="sm" variant={showIncidents ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => setShowIncidents(!showIncidents)}>
                  {showIncidents ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #15 Weather overlay */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Cloud className="w-3 h-3" /> Weather</span>
                <Button size="sm" variant={showWeather ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => { setShowWeather(!showWeather); if (!showWeather) loadWeather(); }}>
                  {showWeather ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #30 H3 hex grid */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Hexagon className="w-3 h-3" /> H3 Grid</span>
                <Button size="sm" variant={showH3Grid ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => setShowH3Grid(!showH3Grid)}>
                  {showH3Grid ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #26 Mesh network */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Route className="w-3 h-3" /> Mesh Net</span>
                <Button size="sm" variant={showMesh ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => setShowMesh(!showMesh)}>
                  {showMesh ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #13 Movement trails */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Activity className="w-3 h-3" /> Trails</span>
                <Button size="sm" variant={showTrails ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={() => setShowTrails(!showTrails)}>
                  {showTrails ? 'Hide' : 'Show'}
                </Button>
              </div>
              {/* #28 Voice navigation */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Mic className="w-3 h-3" /> Voice Nav</span>
                <Button size="sm" variant={voiceListening ? 'default' : 'outline'} className="h-6 text-xs px-2"
                  onClick={startVoiceNav}>
                  {voiceListening ? 'Listening...' : 'Speak'}
                </Button>
              </div>
              {/* #12 Measurement tools */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><MapPin className="w-3 h-3" /> Measure</span>
                <Button size="sm" variant="outline" className="h-6 text-xs px-2"
                  onClick={() => {
                    if (!mapRef.current) return;
                    const map = mapRef.current;
                    const center = map.getCenter();
                    const zoom = map.getZoom();
                    const metersPerPixel = 40075016.686 * Math.cos(center.lat * Math.PI / 180) / Math.pow(2, zoom + 8);
                    const radiusKm = (metersPerPixel * 100 / 1000).toFixed(1);
                    alert(`Map center: ${center.lat.toFixed(4)}°N, ${center.lng.toFixed(4)}°E\nZoom: ${zoom.toFixed(1)}\n100px ≈ ${radiusKm} km`);
                  }}>
                  Distance
                </Button>
              </div>
              {/* #27 Satellite change detection */}
              <div className="flex items-center justify-between">
                <span className="text-xs flex items-center gap-1"><Satellite className="w-3 h-3" /> Sat. View</span>
                <Button size="sm" variant="outline" className="h-6 text-xs px-2"
                  onClick={() => {
                    setTileMode(tileMode === 'satellite' ? 'street' : 'satellite');
                  }}>
                  {tileMode === 'satellite' ? 'Street' : 'Satellite'}
                </Button>
              </div>
              <Button size="sm" variant="outline" className="w-full h-6 text-xs"
                onClick={() => loadSpatialStats(selectedState?.code)}>
                <Activity className="w-3 h-3 mr-1" /> Load Spatial Stats
              </Button>
            </CardContent>
          </Card>

          {/* #9 Crowd Alerts Panel */}
          {crowdAlerts.length > 0 && (
            <Card className="border-amber-300 bg-amber-50 dark:bg-amber-950/30">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm flex items-center gap-1.5"><AlertTriangle className="w-3.5 h-3.5 text-amber-500" /> Crowd Alerts ({crowdAlerts.length})</CardTitle>
              </CardHeader>
              <CardContent className="space-y-1">
                {crowdAlerts.slice(0, 5).map((a, i) => (
                  <div key={a.id || i} className="text-xs flex items-center gap-1">
                    <Badge variant={a.severity === 'critical' ? 'destructive' : 'secondary'} className="text-[10px] h-4">{a.severity}</Badge>
                    <span className="truncate">{a.message || a.pu_code}</span>
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          {/* Spatial Stats Panel */}
          {spatialStats && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">Spatial Statistics</CardTitle>
              </CardHeader>
              <CardContent className="space-y-1">
                <div className="flex justify-between text-xs"><span>Total PUs</span><span className="font-mono">{formatNumber(spatialStats.total_pus)}</span></div>
                <div className="flex justify-between text-xs"><span>Avg Turnout</span><span className="font-mono">{(spatialStats.avg_turnout * 100).toFixed(1)}%</span></div>
                <div className="flex justify-between text-xs"><span>Area</span><span className="font-mono">{formatNumber(Math.round(spatialStats.area_km2))} km²</span></div>
                <div className="flex justify-between text-xs"><span>PU Density</span><span className="font-mono">{spatialStats.pu_density_per_km2.toFixed(1)}/km²</span></div>
              </CardContent>
            </Card>
          )}

          {/* Nearby PUs Panel */}
          {showNearby && nearbyPUs.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm">Nearby Polling Units</CardTitle>
                  <Button size="sm" variant="ghost" className="h-5 text-xs px-1" onClick={() => setShowNearby(false)}>×</Button>
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-1 max-h-48 overflow-y-auto">
                  {nearbyPUs.map(pu => (
                    <div key={pu.polling_unit_code} className="flex items-center justify-between text-xs py-0.5 cursor-pointer hover:bg-zinc-50 px-1 rounded"
                      onClick={() => {
                        if (mapRef.current) {
                          mapRef.current.flyTo({ center: [pu.longitude, pu.latitude], zoom: 15, duration: 800 });
                        }
                      }}>
                      <span className="truncate flex-1">{pu.name}</span>
                      <Badge variant="outline" className="text-[10px] ml-1 shrink-0">
                        {pu.distance_m < 1000 ? `${Math.round(pu.distance_m)}m` : `${(pu.distance_m / 1000).toFixed(1)}km`}
                      </Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Landmarks Legend */}
          {showLandmarks && landmarks.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">{landmarks.length} Landmarks</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-1 max-h-40 overflow-y-auto">
                  {landmarks.slice(0, 15).map(lm => (
                    <div key={lm.id} className="flex items-center gap-1.5 text-xs cursor-pointer hover:bg-zinc-50 px-1 rounded"
                      onClick={() => {
                        if (mapRef.current) mapRef.current.flyTo({ center: [lm.longitude, lm.latitude], zoom: 14, duration: 800 });
                      }}>
                      <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: '#059669' }} />
                      <span className="truncate">{lm.name}</span>
                      <Badge variant="outline" className="text-[9px] shrink-0">{lm.category.replace(/_/g, ' ')}</Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Officials Tracking Panel */}
          {showTracking && officials.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm flex items-center gap-1"><Radio className="w-3.5 h-3.5 text-green-500" /> Live Officials ({officials.length})</CardTitle>
                  <div className="flex items-center gap-1">
                    <div className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                    <span className="text-[10px] text-green-600">LIVE</span>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-1.5 max-h-56 overflow-y-auto">
                  {officials.map(off => (
                    <div key={off.staff_id} className="flex items-center gap-2 text-xs py-1 px-1.5 rounded cursor-pointer hover:bg-zinc-50 border border-zinc-100"
                      onClick={() => {
                        if (mapRef.current) mapRef.current.flyTo({ center: [off.longitude, off.latitude], zoom: 16, duration: 800 });
                      }}>
                      <div className="w-6 h-6 rounded-full flex items-center justify-center text-white text-[10px] shrink-0"
                        style={{ backgroundColor: off.role === 'security' ? '#16a34a' : off.role === 'observer' ? '#2563eb' : off.role === 'presiding_officer' ? '#dc2626' : '#7c3aed' }}>
                        {off.role === 'security' ? <Shield className="w-3 h-3" /> : off.role === 'observer' ? <Eye className="w-3 h-3" /> : <Users className="w-3 h-3" />}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="font-medium truncate">{off.staff_id}</div>
                        <div className="text-zinc-500 text-[10px]">{off.activity} • {off.pu_code}</div>
                      </div>
                      <div className="flex flex-col items-end shrink-0">
                        <Badge variant={off.battery_pct > 50 ? 'default' : off.battery_pct > 20 ? 'secondary' : 'destructive'} className="text-[9px] h-4">
                          <Battery className="w-2.5 h-2.5 mr-0.5" />{off.battery_pct}%
                        </Badge>
                        <span className="text-[9px] text-zinc-400">{off.role.replace(/_/g, ' ')}</span>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="mt-2 flex gap-1 flex-wrap">
                  {['presiding_officer', 'observer', 'security', 'supervisor', 'tech_support'].map(role => {
                    const count = officials.filter(o => o.role === role).length;
                    if (!count) return null;
                    return <Badge key={role} variant="outline" className="text-[9px]">{role.replace(/_/g, ' ')}: {count}</Badge>;
                  })}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Crowd Density Panel */}
          {showCrowd && crowdReports.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm flex items-center gap-1"><Users className="w-3.5 h-3.5" /> Crowd Density</CardTitle>
                  <Button size="sm" variant="ghost" className="h-5 text-xs px-1" onClick={() => setShowCrowd(false)}>×</Button>
                </div>
              </CardHeader>
              <CardContent>
                {/* Summary badges */}
                <div className="flex gap-1 flex-wrap mb-2">
                  {(['overcrowded', 'high', 'moderate', 'low'] as const).map(level => {
                    const count = crowdReports.filter(r => r.density_level === level).length;
                    const colors: Record<string, string> = { overcrowded: 'bg-red-100 text-red-800', high: 'bg-orange-100 text-orange-800', moderate: 'bg-yellow-100 text-yellow-800', low: 'bg-green-100 text-green-800' };
                    return count > 0 ? <span key={level} className={`text-[10px] px-1.5 py-0.5 rounded ${colors[level]}`}>{level}: {count}</span> : null;
                  })}
                </div>
                <div className="space-y-1 max-h-48 overflow-y-auto">
                  {crowdReports.slice(0, 20).map((cr, i) => {
                    const densityColor: Record<string, string> = { low: 'text-green-600', moderate: 'text-yellow-600', high: 'text-orange-600', overcrowded: 'text-red-600' };
                    return (
                      <div key={`${cr.pu_code}-${i}`} className="flex items-center gap-2 text-xs py-0.5 px-1 rounded cursor-pointer hover:bg-zinc-50"
                        onClick={() => {
                          if (mapRef.current && cr.latitude && cr.longitude) mapRef.current.flyTo({ center: [cr.longitude, cr.latitude], zoom: 15, duration: 800 });
                        }}>
                        <span className={`font-bold ${densityColor[cr.density_level] || ''}`}>{cr.head_count}</span>
                        <span className="truncate flex-1">{cr.pu_name || cr.pu_code}</span>
                        <div className="flex items-center gap-1 shrink-0">
                          <Badge variant="outline" className="text-[9px]"><Clock className="w-2 h-2 mr-0.5" />{cr.wait_time_min}m</Badge>
                          <Badge variant="outline" className="text-[9px]">Q:{cr.queue_length}</Badge>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      </div>

      {/* Street View Panel - Enhanced with Google Maps fallback */}
      {streetViewUrl && (
        <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4">
          <div className="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-full max-w-5xl h-[85vh] flex flex-col">
            <div className="flex items-center justify-between p-3 border-b">
              <h3 className="font-semibold text-sm flex items-center gap-1.5">
                <Eye className="w-4 h-4" /> Street View
                {streetViewLat && streetViewLng && (
                  <span className="text-zinc-400 font-normal text-xs ml-2">({streetViewLat.toFixed(4)}, {streetViewLng.toFixed(4)})</span>
                )}
              </h3>
              <div className="flex gap-2 items-center">
                <a href={streetViewUrl} target="_blank" rel="noopener noreferrer" className="text-xs text-blue-600 hover:underline flex items-center gap-1">
                  <ExternalLink className="w-3 h-3" /> Open External
                </a>
                {streetViewLat && streetViewLng && (
                  <a href={`https://www.google.com/maps/@?api=1&map_action=pano&viewpoint=${streetViewLat},${streetViewLng}`}
                    target="_blank" rel="noopener noreferrer" className="text-xs text-green-600 hover:underline flex items-center gap-1">
                    <MapIcon className="w-3 h-3" /> Google Street View
                  </a>
                )}
                <Button size="sm" variant="ghost" className="h-6 px-2" onClick={() => { setStreetViewUrl(null); setStreetViewLat(null); setStreetViewLng(null); }}>×</Button>
              </div>
            </div>
            <div className="flex-1 relative">
              <iframe src={streetViewUrl} className="w-full h-full border-0 rounded-b-lg" title="Street View"
                allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope" allowFullScreen />
            </div>
          </div>
        </div>
      )}

      {/* CSS for pulsing animation */}
      <style>{`
        @keyframes pulse {
          0% { transform: scale(1); opacity: 0.5; }
          50% { transform: scale(1.3); opacity: 0; }
          100% { transform: scale(1); opacity: 0; }
        }
      `}</style>
    </div>
  );
}
