/**
 * Polling Unit Locator Map
 * Interactive Google Maps integration for locating and managing polling units.
 * Data source: live DB via trpc.pollingUnits.list; falls back to demo data when DB is empty.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowLeft, Search, MapPin, Users, RefreshCw, Upload, Database, BarChart2 } from "lucide-react";
import { MarkerClusterer } from "@googlemaps/markerclusterer";
import { Link } from "wouter";
import { MapView } from "@/components/Map";
import { trpc } from "@/lib/trpc";
import { toast } from "sonner";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";

interface PollingUnit {
  id: string;
  name: string;
  lga: string;
  ward: string;
  registeredVoters: number;
  status: "Active" | "Relocated" | "Merged" | "Disputed";
  lat: number;
  lng: number;
  agent: string;
  puCode?: string;
}

const DEMO_UNITS: PollingUnit[] = [
  { id: "pu1", name: "Agodi Gate Primary School", lga: "Ibadan North", ward: "Agodi-Gate", registeredVoters: 842, status: "Active", lat: 7.3986, lng: 3.9007, agent: "Bola Adeyemi" },
  { id: "pu2", name: "Oke-Aremo Town Hall", lga: "Ibadan North", ward: "Oke-Aremo", registeredVoters: 612, status: "Active", lat: 7.4020, lng: 3.8950, agent: "Chukwudi Obi" },
  { id: "pu3", name: "Oke-Padre Community Centre", lga: "Ibadan South-West", ward: "Oke-Padre", registeredVoters: 1100, status: "Relocated", lat: 7.3850, lng: 3.8880, agent: "Fatima Yusuf" },
  { id: "pu4", name: "Egbeda I Primary School", lga: "Egbeda", ward: "Egbeda I", registeredVoters: 1400, status: "Active", lat: 7.3700, lng: 3.8650, agent: "Musa Tanko" },
  { id: "pu5", name: "Akanran Market Square", lga: "Ona-Ara", ward: "Akanran", registeredVoters: 520, status: "Disputed", lat: 7.3200, lng: 3.9300, agent: "Ngozi Eze" },
  { id: "pu6", name: "Iyana-Offa Secondary School", lga: "Lagelu", ward: "Iyana-Offa", registeredVoters: 680, status: "Active", lat: 7.4300, lng: 3.9500, agent: "Ibrahim Garba" },
  { id: "pu7", name: "Bashorun Community Hall", lga: "Ibadan North-East", ward: "Bashorun", registeredVoters: 1500, status: "Active", lat: 7.4100, lng: 3.9200, agent: "Adunola Bello" },
  { id: "pu8", name: "Sango Primary School", lga: "Ibadan North-West", ward: "Sango", registeredVoters: 720, status: "Merged", lat: 7.4050, lng: 3.8800, agent: "Emeka Okonkwo" },
];

const STATUS_COLORS: Record<string, string> = {
  Active: "#22c55e",
  Relocated: "#f59e0b",
  Merged: "#94a3b8",
  Disputed: "#ef4444",
};


// 20 realistic INEC-style sample polling units across 6 states
const SAMPLE_INEC_UNITS = [
  { name: "Garki Area 10 Primary School", puCode: "FCT/AMAC/001", lga: "AMAC", ward: "Garki", latitude: 9.0579, longitude: 7.4951, registeredVoters: 1124 },
  { name: "Wuse Zone 4 Community Hall", puCode: "FCT/AMAC/002", lga: "AMAC", ward: "Wuse", latitude: 9.0765, longitude: 7.4892, registeredVoters: 876 },
  { name: "Maitama District School", puCode: "FCT/AMAC/003", lga: "AMAC", ward: "Maitama", latitude: 9.0820, longitude: 7.5010, registeredVoters: 654 },
  { name: "Gwagwalada Town Hall", puCode: "FCT/GWA/001", lga: "Gwagwalada", ward: "Gwagwalada Central", latitude: 8.9400, longitude: 7.0800, registeredVoters: 2100 },
  { name: "Kuje Market Square PU", puCode: "FCT/KUJ/001", lga: "Kuje", ward: "Kuje Central", latitude: 8.8800, longitude: 7.2300, registeredVoters: 1450 },
  { name: "Agodi Gate Primary School", puCode: "OYO/IBN/001", lga: "Ibadan North", ward: "Agodi-Gate", latitude: 7.3986, longitude: 3.9007, registeredVoters: 842 },
  { name: "Oke-Aremo Town Hall", puCode: "OYO/IBN/002", lga: "Ibadan North", ward: "Oke-Aremo", latitude: 7.4020, longitude: 3.8950, registeredVoters: 612 },
  { name: "Egbeda I Primary School", puCode: "OYO/EGB/001", lga: "Egbeda", ward: "Egbeda I", latitude: 7.3700, longitude: 3.8650, registeredVoters: 1400 },
  { name: "Ikeja GRA Community Hall", puCode: "LAG/IKJ/001", lga: "Ikeja", ward: "GRA", latitude: 6.5960, longitude: 3.3470, registeredVoters: 980 },
  { name: "Surulere Stadium PU", puCode: "LAG/SUR/001", lga: "Surulere", ward: "Surulere Central", latitude: 6.5000, longitude: 3.3500, registeredVoters: 1320 },
  { name: "Oshodi Market Primary School", puCode: "LAG/OSH/001", lga: "Oshodi-Isolo", ward: "Oshodi", latitude: 6.5560, longitude: 3.3500, registeredVoters: 1750 },
  { name: "Kano Municipal Town Hall", puCode: "KAN/KMC/001", lga: "Kano Municipal", ward: "Fagge", latitude: 12.0022, longitude: 8.5920, registeredVoters: 2300 },
  { name: "Nassarawa Primary School Kano", puCode: "KAN/KMC/002", lga: "Kano Municipal", ward: "Nassarawa", latitude: 12.0100, longitude: 8.6100, registeredVoters: 1890 },
  { name: "Dala Community Centre", puCode: "KAN/DAL/001", lga: "Dala", ward: "Dala Central", latitude: 12.0300, longitude: 8.5500, registeredVoters: 1560 },
  { name: "Enugu GRA Primary School", puCode: "ENU/ENG/001", lga: "Enugu North", ward: "GRA", latitude: 6.4698, longitude: 7.5560, registeredVoters: 720 },
  { name: "Ogui Road Community Hall", puCode: "ENU/ENG/002", lga: "Enugu North", ward: "Ogui", latitude: 6.4550, longitude: 7.5400, registeredVoters: 890 },
  { name: "Independence Layout PU", puCode: "ENU/ENS/001", lga: "Enugu South", ward: "Independence Layout", latitude: 6.4400, longitude: 7.5200, registeredVoters: 1100 },
  { name: "Port Harcourt GRA PU", puCode: "RIV/PHC/001", lga: "Port Harcourt", ward: "GRA Phase 1", latitude: 4.8156, longitude: 7.0498, registeredVoters: 1200 },
  { name: "Rumuola Primary School", puCode: "RIV/PHC/002", lga: "Port Harcourt", ward: "Rumuola", latitude: 4.8300, longitude: 7.0600, registeredVoters: 940 },
  { name: "Eleme Town Hall", puCode: "RIV/ELE/001", lga: "Eleme", ward: "Eleme Central", latitude: 4.7800, longitude: 7.1100, registeredVoters: 1650 },
];

export default function PollingUnitLocator() {
  const [selected, setSelected] = useState<PollingUnit | null>(null);
  const [search, setSearch] = useState("");
  const [filterStatus, setFilterStatus] = useState<"all" | PollingUnit["status"]>("all");
  const fileRef = useRef<HTMLInputElement>(null);
  const bulkMut = trpc.pollingUnits.bulkImport.useMutation({
    onSuccess: d => { refetch(); toast.success(`Imported ${d.inserted} polling units`); },
    onError: (e: any) => toast.error(e.message),
  });

  function handleSeedSampleData() {
    if (!profileId) return;
    bulkMut.mutate({ profileId, rows: SAMPLE_INEC_UNITS });
  }

  function parsePUCSV(text: string) {
    const lines = text.trim().split("\n").filter(l => l.trim());
    if (lines.length < 2) return [];
    const headers = lines[0].split(",").map(h => h.trim().toLowerCase().replace(/\s+/g, "_"));
    const col = (names: string[]) => names.map(n => headers.indexOf(n)).find(i => i >= 0) ?? -1;
    const nameIdx = col(["name","unit_name","polling_unit_name","pu_name"]);
    const codeIdx = col(["pu_code","pucode","code","polling_unit_code"]);
    const lgaIdx = col(["lga","local_government"]);
    const wardIdx = col(["ward"]);
    const latIdx = col(["lat","latitude"]);
    const lngIdx = col(["lng","lon","longitude"]);
    const votersIdx = col(["registered_voters","voters","registered"]);
    if (nameIdx < 0) throw new Error("CSV must have a 'name' column");
    return lines.slice(1).map(line => {
      const c = line.split(",").map(s => s.trim().replace(/^"|"$/g, ""));
      return {
        name: c[nameIdx] ?? "",
        puCode: codeIdx >= 0 ? c[codeIdx] : undefined,
        lga: lgaIdx >= 0 ? c[lgaIdx] : undefined,
        ward: wardIdx >= 0 ? c[wardIdx] : undefined,
        latitude: latIdx >= 0 ? parseFloat(c[latIdx]) || undefined : undefined,
        longitude: lngIdx >= 0 ? parseFloat(c[lngIdx]) || undefined : undefined,
        registeredVoters: votersIdx >= 0 ? parseInt(c[votersIdx]) || undefined : undefined,
      };
    }).filter(r => r.name);
  }

  function handleCSVUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file || !profileId) return;
    const reader = new FileReader();
    reader.onload = ev => {
      try {
        const rows = parsePUCSV(ev.target?.result as string);
        if (rows.length === 0) return toast.error("No valid rows found in CSV");
        bulkMut.mutate({ profileId, rows });
      } catch (err: any) { toast.error(err.message ?? "CSV parse error"); }
    };
    reader.readAsText(file);
    e.target.value = "";
  }

  const mapRef = useRef<google.maps.Map | null>(null);
  const markersRef = useRef<google.maps.Marker[]>([]);
  const clustererRef = useRef<MarkerClusterer | null>(null);
  const infoWindowRef = useRef<google.maps.InfoWindow | null>(null);
  const [activeTab, setActiveTab] = useState<'list' | 'lga'>('list');

  const { profileId } = useCandidateProfile();
  const { data: dbUnits = [], isLoading, refetch } = trpc.pollingUnits.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );

  // Map DB rows to local shape; fall back to demo data when DB is empty or no coords
  const dbMapped: PollingUnit[] = dbUnits
    .filter((u: any) => u.lat != null && u.lng != null)
    .map((u: any) => ({
      id: String(u.id),
      name: u.name,
      lga: u.lga ?? "—",
      ward: u.ward ?? "—",
      registeredVoters: u.registeredVoters ?? 0,
      status: (["Active", "Relocated", "Merged", "Disputed"].includes(u.status ?? "")) ? u.status as PollingUnit["status"] : "Active",
      lat: u.lat,
      lng: u.lng,
      agent: u.agentAssigned ?? "Unassigned",
      puCode: u.puCode ?? undefined,
    }));

  const ALL_UNITS = dbMapped.length > 0 ? dbMapped : DEMO_UNITS;
  const usingDemo = dbMapped.length === 0;

  const filtered = ALL_UNITS.filter(pu =>
    (filterStatus === "all" || pu.status === filterStatus) &&
    (search === "" || pu.name.toLowerCase().includes(search.toLowerCase()) || pu.lga.toLowerCase().includes(search.toLowerCase()))
  );

  // Compute LGA summary stats
  const lgaSummary = (() => {
    const map: Record<string, { total: number; active: number; disputed: number; voters: number }> = {};
    ALL_UNITS.forEach(pu => {
      if (!map[pu.lga]) map[pu.lga] = { total: 0, active: 0, disputed: 0, voters: 0 };
      map[pu.lga].total++;
      if (pu.status === "Active") map[pu.lga].active++;
      if (pu.status === "Disputed") map[pu.lga].disputed++;
      map[pu.lga].voters += pu.registeredVoters;
    });
    return Object.entries(map).sort((a, b) => b[1].total - a[1].total);
  })();

  // Rebuild markers whenever the unit list changes
  const buildMarkers = useCallback((map: google.maps.Map, units: PollingUnit[]) => {
    markersRef.current.forEach(m => m.setMap(null));
    markersRef.current = [];
    if (!infoWindowRef.current) infoWindowRef.current = new google.maps.InfoWindow();

    units.forEach(pu => {
      const marker = new google.maps.Marker({
        position: { lat: pu.lat, lng: pu.lng },
        map,
        title: pu.name,
        icon: {
          path: google.maps.SymbolPath.CIRCLE,
          scale: 10,
          fillColor: STATUS_COLORS[pu.status] ?? "#94a3b8",
          fillOpacity: 0.9,
          strokeColor: "#ffffff",
          strokeWeight: 2,
        },
      });
      marker.addListener("click", () => {
        setSelected(pu);
        infoWindowRef.current?.setContent(
          `<div style="font-family:sans-serif;font-size:12px;max-width:200px">
            <strong>${pu.name}</strong><br/>
            ${pu.lga} · ${pu.ward}<br/>
            <span style="color:${STATUS_COLORS[pu.status]};font-weight:bold">${pu.status}</span><br/>
            👤 ${pu.agent}<br/>
            🗳 ${pu.registeredVoters.toLocaleString()} voters
          </div>`
        );
        infoWindowRef.current?.open({ map, anchor: marker });
      });
      markersRef.current.push(marker);
    });
  }, []);

  const handleMapReady = useCallback((map: google.maps.Map) => {
    mapRef.current = map;
    const center = ALL_UNITS.length > 0 ? { lat: ALL_UNITS[0].lat, lng: ALL_UNITS[0].lng } : { lat: 7.3986, lng: 3.9007 };
    map.setCenter(center);
    map.setZoom(12);
    buildMarkers(map, ALL_UNITS);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ALL_UNITS.length, buildMarkers]);

  // Rebuild markers when DB data loads
  useEffect(() => {
    if (mapRef.current && ALL_UNITS.length > 0) {
      buildMarkers(mapRef.current, ALL_UNITS);
    }
  }, [ALL_UNITS.length, buildMarkers]);

  function flyTo(pu: PollingUnit) {
    setSelected(pu);
    if (mapRef.current) {
      mapRef.current.panTo({ lat: pu.lat, lng: pu.lng });
      mapRef.current.setZoom(16);
    }
  }

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "#0d1117", fontFamily: "'IBM Plex Mono', monospace", color: "#e2e8f0" }}>
      <div className="border-b px-6 py-4 flex items-center justify-between flex-shrink-0" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
        <div className="flex items-center gap-4">
          <Link href="/stakeholders"><button className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}><ArrowLeft className="w-3.5 h-3.5" /> Back</button></Link>
          <div>
            <div className="text-xs tracking-widest uppercase mb-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>INEC Campaign Intelligence</div>
            <div className="font-bold text-sm flex items-center gap-2">
              Polling Unit Locator
              {usingDemo && <span className="text-xs px-2 py-0.5 rounded-full font-normal" style={{ background: "oklch(0.22 0.04 60)", color: "oklch(0.70 0.08 60)" }}>Demo data — add units in Volunteer Portal</span>}
              {isLoading && <RefreshCw className="w-3.5 h-3.5 animate-spin" style={{ color: "oklch(0.55 0.01 240)" }} />}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <input ref={fileRef} type="file" accept=".csv" className="hidden" onChange={handleCSVUpload}/>
          <button onClick={() => fileRef.current?.click()} disabled={bulkMut.isPending}
            className="text-xs px-2 py-1 rounded border flex items-center gap-1"
            style={{ borderColor: "oklch(0.35 0.06 140)", color: "oklch(0.65 0.08 140)" }}>
            <Upload className="w-3 h-3" /> {bulkMut.isPending ? "Importing…" : "Import CSV"}
          </button>
          {usingDemo && (
            <button onClick={handleSeedSampleData} disabled={bulkMut.isPending}
              className="text-xs px-2 py-1 rounded border flex items-center gap-1"
              style={{ borderColor: "oklch(0.35 0.08 200)", color: "oklch(0.65 0.10 200)" }}>
              <Database className="w-3 h-3" /> {bulkMut.isPending ? "Seeding…" : "Seed 20 Sample PUs"}
            </button>
          )}
          <button onClick={() => refetch()} className="text-xs px-2 py-1 rounded border flex items-center gap-1" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.55 0.01 240)" }}>
            <RefreshCw className="w-3 h-3" /> Refresh
          </button>
          <div className="flex items-center gap-2 text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>
            {Object.entries(STATUS_COLORS).map(([s, c]) => <span key={s} className="flex items-center gap-1"><span className="w-2.5 h-2.5 rounded-full inline-block" style={{ background: c }} />{s}</span>)}
          </div>
        </div>
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <div className="w-80 border-r flex flex-col flex-shrink-0" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
          {/* Tab switcher */}
          <div className="flex border-b" style={{ borderColor: "oklch(0.20 0.01 240)" }}>
            {([["list", "Units", MapPin], ["lga", "LGA Summary", BarChart2]] as const).map(([tab, label, Icon]) => (
              <button key={tab} onClick={() => setActiveTab(tab)} className="flex-1 flex items-center justify-center gap-1.5 py-2.5 text-xs font-semibold transition-colors"
                style={{ color: activeTab === tab ? "oklch(0.65 0.18 280)" : "oklch(0.45 0.01 240)", borderBottom: activeTab === tab ? "2px solid oklch(0.55 0.18 280)" : "2px solid transparent" }}>
                <Icon className="w-3.5 h-3.5" />{label}
              </button>
            ))}
          </div>

          {activeTab === "list" && (
            <>
              <div className="p-3 border-b space-y-2" style={{ borderColor: "oklch(0.20 0.01 240)" }}>
                <div className="flex items-center gap-2 px-3 py-2 rounded-lg border" style={{ borderColor: "oklch(0.28 0.01 240)", background: "oklch(0.16 0.008 240)" }}>
                  <Search className="w-3.5 h-3.5 flex-shrink-0" style={{ color: "oklch(0.45 0.01 240)" }} />
                  <input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search units…" className="flex-1 text-xs outline-none bg-transparent" style={{ color: "oklch(0.80 0.01 240)" }} />
                </div>
                <div className="flex gap-1.5 flex-wrap">
                  {(["all", "Active", "Relocated", "Merged", "Disputed"] as const).map(s => (
                    <button key={s} onClick={() => setFilterStatus(s)} className="text-xs px-2 py-0.5 rounded-full border capitalize transition-all" style={{ borderColor: filterStatus === s ? "oklch(0.55 0.18 280)" : "oklch(0.28 0.01 240)", color: filterStatus === s ? "oklch(0.65 0.18 280)" : "oklch(0.50 0.01 240)", background: filterStatus === s ? "oklch(0.16 0.04 280)" : "transparent" }}>{s}</button>
                  ))}
                </div>
                <div className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                  {filtered.length} of {ALL_UNITS.length} unit{ALL_UNITS.length !== 1 ? "s" : ""}
                </div>
              </div>
              <div className="flex-1 overflow-y-auto divide-y" style={{ borderColor: "oklch(0.18 0.008 240)" }}>
                {filtered.map(pu => (
                  <button key={pu.id} onClick={() => flyTo(pu)} className="w-full px-3 py-3 text-left hover:bg-white/5 transition-colors" style={{ background: selected?.id === pu.id ? "oklch(0.18 0.01 240)" : "transparent" }}>
                    <div className="flex items-start gap-2.5">
                      <MapPin className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" style={{ color: STATUS_COLORS[pu.status] }} />
                      <div className="flex-1 min-w-0">
                        <div className="text-xs font-bold truncate" style={{ color: "oklch(0.80 0.01 240)" }}>{pu.name}</div>
                        <div className="text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>{pu.lga} · {pu.ward}</div>
                        <div className="flex items-center gap-2 mt-1">
                          <span className="text-xs" style={{ color: "oklch(0.55 0.18 145)" }}><Users className="w-3 h-3 inline mr-0.5" />{pu.registeredVoters.toLocaleString()}</span>
                          <span className="text-xs px-1.5 py-0.5 rounded-full font-bold" style={{ background: (STATUS_COLORS[pu.status] ?? "#94a3b8") + "22", color: STATUS_COLORS[pu.status] ?? "#94a3b8" }}>{pu.status}</span>
                        </div>
                        {pu.puCode && <div className="text-xs mt-0.5" style={{ color: "oklch(0.40 0.01 240)" }}>Code: {pu.puCode}</div>}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </>
          )}

          {activeTab === "lga" && (
            <div className="flex-1 overflow-y-auto p-3 space-y-2">
              <p className="text-xs font-semibold uppercase tracking-wider mb-3" style={{ color: "oklch(0.45 0.01 240)" }}>Units per LGA</p>
              {lgaSummary.map(([lga, stats]) => {
                const riskColor = stats.disputed > 0 ? "#ef4444" : stats.active < stats.total * 0.8 ? "#f59e0b" : "#22c55e";
                const riskLabel = stats.disputed > 0 ? "HIGH" : stats.active < stats.total * 0.8 ? "MED" : "LOW";
                return (
                  <div key={lga} className="rounded-lg p-3 border" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
                    <div className="flex items-center justify-between mb-1.5">
                      <span className="text-xs font-bold truncate" style={{ color: "oklch(0.80 0.01 240)" }}>{lga}</span>
                      <span className="text-xs font-bold px-1.5 py-0.5 rounded" style={{ background: riskColor + "22", color: riskColor }}>{riskLabel}</span>
                    </div>
                    <div className="grid grid-cols-3 gap-1 text-xs">
                      <div><div style={{ color: "oklch(0.45 0.01 240)" }}>Total</div><div className="font-bold" style={{ color: "oklch(0.75 0.01 240)" }}>{stats.total}</div></div>
                      <div><div style={{ color: "oklch(0.45 0.01 240)" }}>Active</div><div className="font-bold" style={{ color: "#22c55e" }}>{stats.active}</div></div>
                      <div><div style={{ color: "oklch(0.45 0.01 240)" }}>Disputed</div><div className="font-bold" style={{ color: stats.disputed > 0 ? "#ef4444" : "oklch(0.45 0.01 240)" }}>{stats.disputed}</div></div>
                    </div>
                    <div className="mt-1.5 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                      <Users className="w-3 h-3 inline mr-0.5" />{stats.voters.toLocaleString()} registered voters
                    </div>
                    {/* Risk bar */}
                    <div className="mt-2 h-1 rounded-full overflow-hidden" style={{ background: "oklch(0.20 0.01 240)" }}>
                      <div className="h-full rounded-full transition-all" style={{ width: `${Math.round(stats.active / stats.total * 100)}%`, background: riskColor }} />
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Map */}
        <div className="flex-1 relative">
          <MapView onMapReady={handleMapReady} className="w-full h-full" />
          {selected && (
            <div className="absolute bottom-4 left-4 right-4 max-w-sm rounded-xl border p-4 shadow-2xl" style={{ borderColor: "oklch(0.28 0.01 240)", background: "oklch(0.14 0.008 240)", backdropFilter: "blur(8px)" }}>
              <div className="flex items-start justify-between mb-2">
                <div className="font-bold text-sm" style={{ color: "oklch(0.85 0.01 240)" }}>{selected.name}</div>
                <button onClick={() => setSelected(null)} className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>✕</button>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                {[
                  ["LGA", selected.lga],
                  ["Ward", selected.ward],
                  ["Registered Voters", selected.registeredVoters.toLocaleString()],
                  ["Assigned Agent", selected.agent],
                  ...(selected.puCode ? [["PU Code", selected.puCode]] : []),
                ].map(([k, v]) => (
                  <div key={k}><div style={{ color: "oklch(0.45 0.01 240)" }}>{k}</div><div className="font-bold" style={{ color: "oklch(0.75 0.01 240)" }}>{v}</div></div>
                ))}
              </div>
              <div className="mt-2 flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full" style={{ background: STATUS_COLORS[selected.status] }} />
                <span className="text-xs font-bold" style={{ color: STATUS_COLORS[selected.status] }}>{selected.status}</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
