/**
 * LGA Drill-Down Component
 * Shows ward-level community leaders for a selected state and LGA
 */
import { useState, useMemo } from "react";
import { motion } from "framer-motion";
import { MapPin, Users, ChevronDown, Search, Building2, Star } from "lucide-react";
import type { LGALeader } from "./StakeholderTypes";

// LGA data for all 36 states + FCT (representative sample — full DB in production)
const STATE_LGAS: Record<string, string[]> = {
  FCT:  ["Abaji", "Abuja Municipal", "Bwari", "Gwagwalada", "Kuje", "Kwali"],
  LAG:  ["Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry", "Epe", "Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe", "Lagos Island", "Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere"],
  KAN:  ["Dala", "Fagge", "Gwale", "Kano Municipal", "Nassarawa", "Tarauni", "Ungogo", "Kumbotso", "Dawakin Tofa", "Tofa", "Rimin Gado", "Bagwai", "Gezawa", "Gabasawa", "Minjibir", "Warawa", "Gwarzo", "Karaye", "Rogo", "Kibiya", "Rano", "Tudun Wada", "Doguwa", "Kiru", "Bebeji", "Sumaila", "Garko", "Albasu", "Gaya", "Ajingi", "Wudil", "Takai", "Bunkure", "Tsanyawa", "Shanono", "Garo", "Madobi", "Makoda", "Kunchi", "Bichi", "Kabo", "Dambatta", "Miga", "Dawakin Kudu"],
  RIV:  ["Abua/Odual", "Ahoada East", "Ahoada West", "Akuku-Toru", "Andoni", "Asari-Toru", "Bonny", "Degema", "Eleme", "Emohua", "Etche", "Gokana", "Ikwerre", "Khana", "Obio/Akpor", "Ogba/Egbema/Ndoni", "Ogu/Bolo", "Okrika", "Omuma", "Opobo/Nkoro", "Oyigbo", "Port Harcourt", "Tai"],
  OYO:  ["Afijio", "Akinyele", "Atiba", "Atisbo", "Egbeda", "Ibadan North", "Ibadan North-East", "Ibadan North-West", "Ibadan South-East", "Ibadan South-West", "Ibarapa Central", "Ibarapa East", "Ibarapa North", "Ido", "Irepo", "Iseyin", "Itesiwaju", "Iwajowa", "Kajola", "Lagelu", "Ogbomosho North", "Ogbomosho South", "Ogo Oluwa", "Olorunsogo", "Oluyole", "Ona Ara", "Orelope", "Ori Ire", "Oyo East", "Oyo West", "Saki East", "Saki West", "Surulere"],
  ANM:  ["Aguata", "Anambra East", "Anambra West", "Anaocha", "Awka North", "Awka South", "Ayamelum", "Dunukofia", "Ekwusigo", "Idemili North", "Idemili South", "Ihiala", "Njikoka", "Nnewi North", "Nnewi South", "Ogbaru", "Onitsha North", "Onitsha South", "Orumba North", "Orumba South", "Oyi"],
  ENU:  ["Aninri", "Awgu", "Enugu East", "Enugu North", "Enugu South", "Ezeagu", "Igbo Etiti", "Igbo Eze North", "Igbo Eze South", "Isi Uzo", "Nkanu East", "Nkanu West", "Nsukka", "Oji River", "Udenu", "Udi", "Uzo Uwani"],
  DEL:  ["Aniocha North", "Aniocha South", "Bomadi", "Burutu", "Ethiope East", "Ethiope West", "Ika North East", "Ika South", "Isoko North", "Isoko South", "Ndokwa East", "Ndokwa West", "Okpe", "Oshimili North", "Oshimili South", "Patani", "Sapele", "Udu", "Ughelli North", "Ughelli South", "Ukwuani", "Uvwie", "Warri North", "Warri South", "Warri South West"],
  KAT:  ["Bakori", "Batagarawa", "Batsari", "Baure", "Bindawa", "Charanchi", "Dan Musa", "Dandume", "Danja", "Daura", "Dutsi", "Dutsin Ma", "Faskari", "Funtua", "Ingawa", "Jibia", "Kafur", "Kaita", "Kankara", "Kankia", "Katsina", "Kurfi", "Kusada", "Mai'Adua", "Malumfashi", "Mani", "Mashi", "Matazu", "Musawa", "Rimi", "Sabuwa", "Safana", "Sandamu", "Zango"],
  BOR:  ["Abadam", "Askira/Uba", "Bama", "Bayo", "Biu", "Chibok", "Damboa", "Dikwa", "Gubio", "Guzamala", "Gwoza", "Hawul", "Jere", "Kaga", "Kala/Balge", "Konduga", "Kukawa", "Kwaya Kusar", "Mafa", "Magumeri", "Maiduguri", "Marte", "Mobbar", "Monguno", "Ngala", "Nganzai", "Shani"],
};

// Generate representative ward-level leaders for a given LGA
function generateLGALeaders(state: string, lga: string): LGALeader[] {
  const roles = [
    { role: "LGA Chairman", category: "Government", influence: "High" as const, contact: "Official LGA Secretariat" },
    { role: "Ward Head (Bulama/Dagaci)", category: "Traditional Leaders", influence: "High" as const, contact: "Community visit required" },
    { role: "LGA Market Women Leader (Iyaloja)", category: "Women", influence: "High" as const, contact: "Market visit" },
    { role: "LGA Youth Leader (NANS/NUJ)", category: "Youth", influence: "Medium" as const, contact: "Phone / WhatsApp" },
    { role: "LGA Farmers Cooperative Chair", category: "Agriculture", influence: "High" as const, contact: "Farm cooperative office" },
    { role: "LGA NLC Branch Secretary", category: "Labour", influence: "Medium" as const, contact: "Union office" },
    { role: "LGA CAN Chairman", category: "Religious", influence: "High" as const, contact: "Church visit" },
    { role: "LGA JNI/Mosque Committee Chair", category: "Religious", influence: "High" as const, contact: "Mosque visit" },
    { role: "Community Development Committee (CDC) Chair", category: "Civil Society", influence: "Medium" as const, contact: "Community hall" },
    { role: "LGA NUT Branch Chairman", category: "Professional", influence: "Medium" as const, contact: "School visit" },
    { role: "Okada/Tricycle Park Chairman", category: "Youth", influence: "Medium" as const, contact: "Motor park visit" },
    { role: "LGA Women Association President", category: "Women", influence: "Medium" as const, contact: "Women's hall" },
  ];

  return roles.map((r, i) => ({
    id: `${state}-${lga}-${i}`.replace(/\s+/g, "-").toLowerCase(),
    lga,
    state,
    name: "Contact via official channels",
    role: r.role,
    category: r.category,
    influence_level: r.influence,
    contact_method: r.contact,
    notes: `Engage through the ${r.role} for ${lga} LGA voter mobilisation. Priority: ${r.influence}.`,
  }));
}

const INFLUENCE_COLORS = {
  High:   "bg-emerald-900/40 text-emerald-300 border-emerald-700/50",
  Medium: "bg-amber-900/40 text-amber-300 border-amber-700/50",
  Low:    "bg-slate-900/40 text-slate-300 border-slate-700/50",
};

interface Props {
  stateCode: string;
  stateName: string;
}

export default function LGADrillDown({ stateCode, stateName }: Props) {
  const lgas = STATE_LGAS[stateCode] ?? STATE_LGAS["FCT"];
  const [selectedLGA, setSelectedLGA] = useState(lgas[0]);
  const [searchQuery, setSearchQuery] = useState("");
  const [filterCategory, setFilterCategory] = useState("All");

  const leaders = useMemo(() => generateLGALeaders(stateCode, selectedLGA), [stateCode, selectedLGA]);

  const categories = useMemo(() => {
    const cats = new Set(leaders.map(l => l.category));
    return ["All", ...Array.from(cats)];
  }, [leaders]);

  const filtered = leaders.filter(l => {
    const matchCat = filterCategory === "All" || l.category === filterCategory;
    const matchSearch = searchQuery === "" ||
      l.role.toLowerCase().includes(searchQuery.toLowerCase()) ||
      l.category.toLowerCase().includes(searchQuery.toLowerCase());
    return matchCat && matchSearch;
  });

  return (
    <div className="flex flex-col gap-4">
      <div>
        <div className="text-sm font-bold mb-1" style={{ color: "oklch(0.88 0.005 240)" }}>LGA Drill-Down — {stateName}</div>
        <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{lgas.length} LGAs · Select an LGA to view ward-level community leaders</div>
      </div>

      {/* LGA selector */}
      <div className="flex gap-2">
        <div className="relative flex-shrink-0">
          <select
            value={selectedLGA}
            onChange={e => setSelectedLGA(e.target.value)}
            className="appearance-none pl-3 pr-8 py-2 text-sm rounded border"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
          >
            {lgas.map(lga => <option key={lga} value={lga}>{lga}</option>)}
          </select>
          <ChevronDown className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 pointer-events-none" style={{ color: "oklch(0.55 0.01 240)" }} />
        </div>
        <div className="flex items-center gap-2 flex-1 px-3 py-2 rounded border" style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)" }}>
          <Search className="w-3.5 h-3.5" style={{ color: "oklch(0.55 0.01 240)" }} />
          <input
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Search roles..."
            className="flex-1 text-xs bg-transparent outline-none"
            style={{ color: "oklch(0.88 0.005 240)" }}
          />
        </div>
      </div>

      {/* Category filter */}
      <div className="flex flex-wrap gap-1">
        {categories.map(cat => (
          <button
            key={cat}
            onClick={() => setFilterCategory(cat)}
            className="px-2 py-0.5 text-xs rounded border transition-all"
            style={{
              background: filterCategory === cat ? "oklch(0.55 0.18 145)" : "oklch(0.18 0.008 240)",
              color: filterCategory === cat ? "white" : "oklch(0.55 0.01 240)",
              borderColor: filterCategory === cat ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)",
            }}
          >{cat}</button>
        ))}
      </div>

      {/* LGA header */}
      <div className="rounded border p-3 flex items-center gap-3" style={{ background: "oklch(0.18 0.012 240)", borderColor: "oklch(0.35 0.12 145)" }}>
        <MapPin className="w-5 h-5 flex-shrink-0" style={{ color: "oklch(0.65 0.18 145)" }} />
        <div>
          <div className="text-sm font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>{selectedLGA} LGA — {stateName}</div>
          <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{filtered.length} key community leaders identified</div>
        </div>
      </div>

      {/* Leaders grid */}
      <div className="grid grid-cols-2 gap-2">
        {filtered.map((leader, i) => (
          <motion.div
            key={leader.id}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.04 }}
            className="rounded border p-3"
            style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
          >
            <div className="flex items-start justify-between gap-2 mb-2">
              <div className="flex items-center gap-1.5">
                {leader.influence_level === "High" && <Star className="w-3 h-3 flex-shrink-0" style={{ color: "oklch(0.75 0.18 80)" }} fill="oklch(0.75 0.18 80)" />}
                <Building2 className="w-3.5 h-3.5 flex-shrink-0" style={{ color: "oklch(0.55 0.01 240)" }} />
              </div>
              <span className={`text-xs px-1.5 py-0.5 rounded border ${INFLUENCE_COLORS[leader.influence_level]}`}>
                {leader.influence_level}
              </span>
            </div>
            <div className="text-xs font-bold mb-1 leading-tight" style={{ color: "oklch(0.88 0.005 240)" }}>{leader.role}</div>
            <div className="text-xs mb-2" style={{ color: "oklch(0.55 0.18 145)" }}>{leader.category}</div>
            <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
              <span className="font-bold">Contact: </span>{leader.contact_method}
            </div>
          </motion.div>
        ))}
      </div>
    </div>
  );
}
