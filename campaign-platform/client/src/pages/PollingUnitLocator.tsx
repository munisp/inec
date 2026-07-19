/**
 * Polling Unit Locator Map
 * Interactive Google Maps integration for locating and managing polling units.
 */
import { useCallback, useRef, useState } from "react";
import { ArrowLeft, Search, MapPin, Users, CheckCircle2, AlertTriangle } from "lucide-react";
import { Link } from "wouter";
import { MapView } from "@/components/Map";

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
}

const POLLING_UNITS: PollingUnit[] = [
  { id: "pu1", name: "Agodi Gate Primary School", lga: "Ibadan North", ward: "Agodi-Gate", registeredVoters: 842, status: "Active", lat: 7.3986, lng: 3.9007, agent: "Bola Adeyemi" },
  { id: "pu2", name: "Oke-Aremo Town Hall", lga: "Ibadan North", ward: "Oke-Aremo", registeredVoters: 612, status: "Active", lat: 7.4020, lng: 3.8950, agent: "Chukwudi Obi" },
  { id: "pu3", name: "Oke-Padre Community Centre", lga: "Ibadan South-West", ward: "Oke-Padre", registeredVoters: 1100, status: "Relocated", lat: 7.3850, lng: 3.8880, agent: "Fatima Yusuf" },
  { id: "pu4", name: "Egbeda I Primary School", lga: "Egbeda", ward: "Egbeda I", registeredVoters: 1400, status: "Active", lat: 7.3700, lng: 3.8650, agent: "Musa Tanko" },
  { id: "pu5", name: "Akanran Market Square", lga: "Ona-Ara", ward: "Akanran", registeredVoters: 520, status: "Disputed", lat: 7.3200, lng: 3.9300, agent: "Ngozi Eze" },
  { id: "pu6", name: "Iyana-Offa Secondary School", lga: "Lagelu", ward: "Iyana-Offa", registeredVoters: 680, status: "Active", lat: 7.4300, lng: 3.9500, agent: "Ibrahim Garba" },
  { id: "pu7", name: "Bashorun Community Hall", lga: "Ibadan North-East", ward: "Bashorun", registeredVoters: 1500, status: "Active", lat: 7.4100, lng: 3.9200, agent: "Adunola Bello" },
  { id: "pu8", name: "Sango Primary School", lga: "Ibadan North-West", ward: "Sango", registeredVoters: 720, status: "Merged", lat: 7.4050, lng: 3.8800, agent: "Emeka Okonkwo" },
];

const STATUS_COLORS = { Active: "#22c55e", Relocated: "#f59e0b", Merged: "#94a3b8", Disputed: "#ef4444" };

export default function PollingUnitLocator() {
  const [selected, setSelected] = useState<PollingUnit | null>(null);
  const [search, setSearch] = useState("");
  const [filterStatus, setFilterStatus] = useState<"all" | PollingUnit["status"]>("all");
  const mapRef = useRef<google.maps.Map | null>(null);
  const markersRef = useRef<google.maps.Marker[]>([]);

  const filtered = POLLING_UNITS.filter(pu =>
    (filterStatus === "all" || pu.status === filterStatus) &&
    (search === "" || pu.name.toLowerCase().includes(search.toLowerCase()) || pu.lga.toLowerCase().includes(search.toLowerCase()))
  );

  const handleMapReady = useCallback((map: google.maps.Map) => {
    mapRef.current = map;
    map.setCenter({ lat: 7.3986, lng: 3.9007 });
    map.setZoom(12);

    markersRef.current.forEach(m => m.setMap(null));
    markersRef.current = [];

    POLLING_UNITS.forEach(pu => {
      const marker = new google.maps.Marker({
        position: { lat: pu.lat, lng: pu.lng },
        map,
        title: pu.name,
        icon: {
          path: google.maps.SymbolPath.CIRCLE,
          scale: 10,
          fillColor: STATUS_COLORS[pu.status],
          fillOpacity: 0.9,
          strokeColor: "#ffffff",
          strokeWeight: 2,
        },
      });
      marker.addListener("click", () => setSelected(pu));
      markersRef.current.push(marker);
    });
  }, []);

  function flyTo(pu: PollingUnit) {
    setSelected(pu);
    if (mapRef.current) {
      mapRef.current.panTo({ lat: pu.lat, lng: pu.lng });
      mapRef.current.setZoom(15);
    }
  }

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "#0d1117", fontFamily: "'IBM Plex Mono', monospace", color: "#e2e8f0" }}>
      <div className="border-b px-6 py-4 flex items-center justify-between flex-shrink-0" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
        <div className="flex items-center gap-4">
          <Link href="/stakeholders"><button className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}><ArrowLeft className="w-3.5 h-3.5" /> Back</button></Link>
          <div><div className="text-xs tracking-widest uppercase mb-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>INEC Campaign Intelligence</div><div className="font-bold text-sm">Polling Unit Locator</div></div>
        </div>
        <div className="flex items-center gap-2 text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>
          {Object.entries(STATUS_COLORS).map(([s, c]) => <span key={s} className="flex items-center gap-1"><span className="w-2.5 h-2.5 rounded-full inline-block" style={{ background: c }} />{s}</span>)}
        </div>
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <div className="w-80 border-r flex flex-col flex-shrink-0" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
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
                      <span className="text-xs px-1.5 py-0.5 rounded-full font-bold" style={{ background: STATUS_COLORS[pu.status] + "22", color: STATUS_COLORS[pu.status] }}>{pu.status}</span>
                    </div>
                  </div>
                </div>
              </button>
            ))}
          </div>
        </div>

        {/* Map */}
        <div className="flex-1 relative">
          <MapView onMapReady={handleMapReady} className="w-full h-full" />
          {selected && (
            <div className="absolute bottom-4 left-4 right-4 max-w-sm rounded-xl border p-4 shadow-2xl" style={{ borderColor: "oklch(0.28 0.01 240)", background: "oklch(0.14 0.008 240)/95", backdropFilter: "blur(8px)" }}>
              <div className="flex items-start justify-between mb-2">
                <div className="font-bold text-sm" style={{ color: "oklch(0.85 0.01 240)" }}>{selected.name}</div>
                <button onClick={() => setSelected(null)} className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>✕</button>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                {[["LGA", selected.lga], ["Ward", selected.ward], ["Registered Voters", selected.registeredVoters.toLocaleString()], ["Assigned Agent", selected.agent]].map(([k, v]) => (
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
