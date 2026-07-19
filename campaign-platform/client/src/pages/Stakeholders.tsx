/**
 * Stakeholder Engagement Recommendation Engine
 * Design: Civic Data Observatory — dark charcoal sidebar, warm parchment content area
 * Shows prioritised stakeholder groups for a candidate by state, office, and profile
 */
import { useState, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  Users, MapPin, Briefcase, ChevronRight, Star, Search,
  Building2, Church, Wheat, Scale, GraduationCap, Heart,
  Truck, Globe, Shield, UserCheck, Filter, Info, ArrowRight,
  AlertCircle, CheckCircle2, MessageCircle
} from "lucide-react";
import EngagementCalendar from "../components/EngagementCalendar";
import LGADrillDown from "../components/LGADrillDown";
import StakeholderCRM from "../components/StakeholderCRM";
import EngagementDashboard from "../components/EngagementDashboard";
import type { Stakeholder as StakeholderType } from "../components/StakeholderTypes";
import type { CRMContact } from "../components/StakeholderTypes";
import StakeholderNetworkGraph from "../components/StakeholderNetworkGraph";
import CandidateComparison from "../components/CandidateComparison";
import SentimentFeed from "../components/SentimentFeed";
import StakeholderBriefPDF from "../components/StakeholderBriefPDF";
import { useNotificationReminders } from "../hooks/useNotificationReminders";
import { Bell, BellOff } from "lucide-react";

// Use the shared type
type Stakeholder = StakeholderType;

// ── Embedded stakeholder data (mirrors stakeholder_engine.py) ─────────────────
const STATES = [
  { code: "FCT", name: "FCT — Abuja",    zone: "North-Central" },
  { code: "LAG", name: "Lagos",           zone: "South-West"    },
  { code: "KAN", name: "Kano",           zone: "North-West"    },
  { code: "RIV", name: "Rivers",         zone: "South-South"   },
  { code: "OYO", name: "Oyo",            zone: "South-West"    },
  { code: "KAT", name: "Katsina",        zone: "North-West"    },
  { code: "ANM", name: "Anambra",        zone: "South-East"    },
  { code: "BOR", name: "Borno",          zone: "North-East"    },
  { code: "DEL", name: "Delta",          zone: "South-South"   },
  { code: "IMO", name: "Imo",            zone: "South-East"    },
  { code: "AKW", name: "Akwa Ibom",      zone: "South-South"   },
  { code: "EDO", name: "Edo",            zone: "South-South"   },
  { code: "KOG", name: "Kogi",           zone: "North-Central" },
  { code: "BNU", name: "Benue",          zone: "North-Central" },
  { code: "PLT", name: "Plateau",        zone: "North-Central" },
  { code: "SOK", name: "Sokoto",         zone: "North-West"    },
  { code: "OGU", name: "Ogun",           zone: "South-West"    },
  { code: "ENU", name: "Enugu",          zone: "South-East"    },
  { code: "ABI", name: "Abia",           zone: "South-East"    },
  { code: "EBO", name: "Ebonyi",         zone: "South-East"    },
  { code: "EKI", name: "Ekiti",          zone: "South-West"    },
  { code: "OND", name: "Ondo",           zone: "South-West"    },
  { code: "OSU", name: "Osun",           zone: "South-West"    },
  { code: "KWA", name: "Kwara",          zone: "North-Central" },
  { code: "NGR", name: "Niger",          zone: "North-Central" },
  { code: "NAS", name: "Nasarawa",       zone: "North-Central" },
  { code: "BAU", name: "Bauchi",         zone: "North-East"    },
  { code: "GOM", name: "Gombe",          zone: "North-East"    },
  { code: "TAR", name: "Taraba",         zone: "North-East"    },
  { code: "YOB", name: "Yobe",           zone: "North-East"    },
  { code: "ADA", name: "Adamawa",        zone: "North-East"    },
  { code: "JIG", name: "Jigawa",         zone: "North-West"    },
  { code: "KBB", name: "Kebbi",          zone: "North-West"    },
  { code: "ZAM", name: "Zamfara",        zone: "North-West"    },
  { code: "BAY", name: "Bayelsa",        zone: "South-South"   },
  { code: "CRS", name: "Cross River",    zone: "South-South"   },
];

const OFFICES = ["President", "Governor", "Senator", "House", "LGA"];

const CATEGORY_ICONS: Record<string, React.ReactNode> = {
  "Youth":               <Users className="w-4 h-4" />,
  "Women":               <Heart className="w-4 h-4" />,
  "Traditional Leaders": <Building2 className="w-4 h-4" />,
  "Religious":           <Church className="w-4 h-4" />,
  "Professional":        <Briefcase className="w-4 h-4" />,
  "Labour":              <Scale className="w-4 h-4" />,
  "Civil Society":       <Shield className="w-4 h-4" />,
  "Agriculture":         <Wheat className="w-4 h-4" />,
  "Commerce":            <Globe className="w-4 h-4" />,
  "Diaspora":            <Globe className="w-4 h-4" />,
  "Inclusion":           <UserCheck className="w-4 h-4" />,
  "Ethnic/Regional":     <MapPin className="w-4 h-4" />,
  "Pastoral":            <Truck className="w-4 h-4" />,
};

const CATEGORY_COLORS: Record<string, string> = {
  "Youth":               "bg-emerald-900/40 text-emerald-300 border-emerald-700/50",
  "Women":               "bg-rose-900/40 text-rose-300 border-rose-700/50",
  "Traditional Leaders": "bg-amber-900/40 text-amber-300 border-amber-700/50",
  "Religious":           "bg-violet-900/40 text-violet-300 border-violet-700/50",
  "Professional":        "bg-sky-900/40 text-sky-300 border-sky-700/50",
  "Labour":              "bg-orange-900/40 text-orange-300 border-orange-700/50",
  "Civil Society":       "bg-teal-900/40 text-teal-300 border-teal-700/50",
  "Agriculture":         "bg-lime-900/40 text-lime-300 border-lime-700/50",
  "Commerce":            "bg-cyan-900/40 text-cyan-300 border-cyan-700/50",
  "Diaspora":            "bg-indigo-900/40 text-indigo-300 border-indigo-700/50",
  "Inclusion":           "bg-pink-900/40 text-pink-300 border-pink-700/50",
  "Ethnic/Regional":     "bg-yellow-900/40 text-yellow-300 border-yellow-700/50",
  "Pastoral":            "bg-stone-900/40 text-stone-300 border-stone-700/50",
};

// ── Full stakeholder database ─────────────────────────────────────────────────
const ALL_STAKEHOLDERS: Stakeholder[] = [
  {
    id: "trad_rulers", name: "State Council of Traditional Rulers",
    category: "Traditional Leaders", subcategory: "Royal Council",
    reach_pct: 12.0, priority: 1,
    engagement_method: ["Royal palace visits", "Chieftaincy title acceptance", "Community development donations"],
    talking_points: ["Traditional institution recognition and funding", "Land rights and community ownership protection", "Rural infrastructure — roads, water, electricity", "Cultural heritage preservation and tourism"],
    cultural_protocol: "NEVER sit higher than the Oba/Emir/Igwe. Remove shoes where required. Prostrate or kneel as culturally appropriate. Bring kola nuts, palm wine, or appropriate gifts. Address by full title.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 10 },
    best_engagement_time: "Morning durbar hours or after Friday prayers for Muslim rulers",
    key_ask: "Royal endorsement — the single most powerful voter influence mechanism in rural Nigeria",
  },
  {
    id: "village_heads", name: "Village Heads, Ward Heads & District Officers",
    category: "Traditional Leaders", subcategory: "Grassroots",
    reach_pct: 15.0, priority: 1,
    engagement_method: ["Village square meetings", "Community project commissioning", "Direct household visits"],
    talking_points: ["Borehole and clean water access", "Rural road grading and bridge repair", "Primary school renovation", "Community health centre staffing"],
    cultural_protocol: "Arrive with the LGA chairman or a known community member. Bring practical gifts (food items, building materials). Speak to the community in their language.",
    office_relevance: { President: 5, Governor: 8, Senator: 7, House: 9, LGA: 10 },
    best_engagement_time: "Evening community meetings (after farm work)",
    key_ask: "Polling unit-level voter mobilisation and election day logistics",
  },
  {
    id: "market_women", name: "Market Women Associations (Iyaloja/State Market Union)",
    category: "Women", subcategory: "Traders",
    reach_pct: 9.1, priority: 1,
    engagement_method: ["Market visits", "Trader loan pledges", "Market infrastructure promises"],
    talking_points: ["Market reconstruction and stall allocation reform", "Trader micro-credit and cooperative loans", "Levy and tax reduction for small traders", "Security at markets and anti-extortion enforcement", "Cold chain and storage infrastructure"],
    cultural_protocol: "Visit the market physically — never invite them to your office first. Bring the Market Queen (Iyaloja/Madame) a gift. Walk through the market stalls.",
    office_relevance: { President: 7, Governor: 10, Senator: 8, House: 9, LGA: 10 },
    best_engagement_time: "Market days (varies by LGA — confirm locally)",
    key_ask: "Market-level voter mobilisation — the most influential ground network in Nigeria",
  },
  {
    id: "ncws", name: "National Council of Women's Societies (NCWS)",
    category: "Women", subcategory: "National Body",
    reach_pct: 8.4, priority: 1,
    engagement_method: ["Women's town halls", "Market outreach", "Radio programmes"],
    talking_points: ["35% affirmative action in government appointments", "Gender-based violence legislation", "Maternal healthcare and free antenatal care", "Women's economic empowerment and cooperative loans", "Girl-child education and school feeding programme"],
    cultural_protocol: "Address the State Chairperson by title. Acknowledge women's contributions before making requests. Bring female surrogates to the meeting.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 9, LGA: 8 },
    best_engagement_time: "Tuesday and Thursday mornings (market days in most states)",
    key_ask: "Women's bloc voter mobilisation — the largest single voting bloc in Nigeria",
  },
  {
    id: "jni", name: "Jama'atu Nasril Islam (JNI) — Supreme Islamic Council",
    category: "Religious", subcategory: "Islamic",
    reach_pct: 10.5, priority: 1,
    engagement_method: ["Friday mosque visits", "Ramadan welfare donations", "Islamic school support"],
    talking_points: ["Almajiri school reform and integration", "Islamic banking and halal finance", "Pilgrimage (Hajj) subsidy and welfare", "Zakat fund and Islamic social welfare", "Northern security and banditry eradication"],
    cultural_protocol: "Engage through the Sultan of Sokoto's office for national campaigns. Never schedule meetings during prayer times. Dress modestly.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 9 },
    best_engagement_time: "After Jumu'ah (Friday prayer) or early morning",
    key_ask: "Mosque-based voter education and Friday sermon endorsement",
  },
  {
    id: "can", name: "Christian Association of Nigeria (CAN) — State Chapter",
    category: "Religious", subcategory: "Christian",
    reach_pct: 10.5, priority: 1,
    engagement_method: ["Church visits", "Sunday school partnerships", "Christian welfare donations"],
    talking_points: ["Religious freedom and church security", "Christian education and mission school funding", "Anti-kidnapping and farmer-herder conflict resolution", "Poverty alleviation and church welfare programmes"],
    cultural_protocol: "Request audience through the State CAN Chairman. Attend a church service. Never make political speeches during worship.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 9 },
    best_engagement_time: "Sunday afternoons or midweek fellowship",
    key_ask: "Church-based voter registration and Sunday announcement endorsements",
  },
  {
    id: "pfn", name: "Pentecostal Fellowship of Nigeria (PFN)",
    category: "Religious", subcategory: "Christian — Pentecostal",
    reach_pct: 7.8, priority: 2,
    engagement_method: ["Crusade sponsorship", "Youth church programmes", "Prosperity gospel alignment"],
    talking_points: ["Economic prosperity and job creation", "Anti-corruption and transparency", "Family values and social welfare", "Education and scholarship programmes"],
    cultural_protocol: "Engage through the General Overseer or Senior Pastor. Attend a programme before requesting endorsement.",
    office_relevance: { President: 9, Governor: 9, Senator: 8, House: 8, LGA: 7 },
    best_engagement_time: "Friday night vigil or Sunday service",
    key_ask: "Congregation voter mobilisation and social media amplification",
  },
  {
    id: "farmers", name: "All Farmers Association of Nigeria (AFAN) — State Chapter",
    category: "Agriculture", subcategory: "Farmers",
    reach_pct: 11.2, priority: 1,
    engagement_method: ["Farm visits", "Fertiliser donation", "Cooperative loan pledges"],
    talking_points: ["Fertiliser subsidy and distribution reform", "Farmer-herder conflict resolution and security", "Irrigation infrastructure and dam rehabilitation", "Agricultural credit and cooperative loans", "Food storage and commodity board revival"],
    cultural_protocol: "Visit farms and rural communities. Dress simply. Bring practical gifts (seeds, fertiliser, farm tools). Speak about food security in concrete terms.",
    office_relevance: { President: 9, Governor: 10, Senator: 8, House: 9, LGA: 10 },
    best_engagement_time: "Post-harvest season (Oct–Dec) or pre-planting (Feb–Mar)",
    key_ask: "Rural voter mobilisation — farmers represent the largest single occupational group of voters",
  },
  {
    id: "nlc", name: "Nigeria Labour Congress (NLC) — State Council",
    category: "Labour", subcategory: "General Labour",
    reach_pct: 5.5, priority: 1,
    engagement_method: ["May Day rally participation", "Worker welfare pledges", "Minimum wage commitment"],
    talking_points: ["Minimum wage increase and implementation", "Workers' rights and anti-casualisation", "Pension reform and gratuity payment", "Occupational health and safety legislation"],
    cultural_protocol: "Attend May Day rallies. Engage the State Chairman. Labour leaders are politically sophisticated — bring detailed policy positions.",
    office_relevance: { President: 10, Governor: 9, Senator: 8, House: 7, LGA: 6 },
    best_engagement_time: "May Day (May 1) and union congress periods",
    key_ask: "Organised labour endorsement — covers millions of formal sector workers",
  },
  {
    id: "nans", name: "National Association of Nigerian Students (NANS)",
    category: "Youth", subcategory: "Student Body",
    reach_pct: 6.8, priority: 1,
    engagement_method: ["Campus rallies", "Debate sponsorship", "Scholarship pledges"],
    talking_points: ["University funding and ASUU strike resolution", "Student loan scheme expansion", "Campus security and hostel infrastructure", "Graduate employment guarantee programme"],
    cultural_protocol: "Engage the NANS Senate President and zone coordinators. Avoid making promises you cannot keep — students fact-check.",
    office_relevance: { President: 10, Governor: 9, Senator: 8, House: 7, LGA: 4 },
    best_engagement_time: "Semester periods (avoid exam weeks)",
    key_ask: "Campus voter registration drives and first-time voter mobilisation",
  },
  {
    id: "nys", name: "NYSC Alumni Association — State Chapter",
    category: "Youth", subcategory: "National Service",
    reach_pct: 4.2, priority: 1,
    engagement_method: ["Town hall meetings", "Social media campaigns", "Skill acquisition partnerships"],
    talking_points: ["Youth employment and entrepreneurship fund", "NYSC reform and skills-to-jobs pipeline", "Digital economy and tech hub investments", "Student loan accessibility"],
    cultural_protocol: "Address the State Coordinator first. Acknowledge service to the nation before policy discussion.",
    office_relevance: { President: 10, Governor: 9, Senator: 8, House: 7, LGA: 5 },
    best_engagement_time: "Weekends and passing-out parade periods",
    key_ask: "Volunteer mobilisation and polling unit coverage on Election Day",
  },
  {
    id: "okada", name: "Commercial Motorcyclists & Tricycle Operators (NURTW/RTEAN)",
    category: "Youth", subcategory: "Transport Workers",
    reach_pct: 7.3, priority: 1,
    engagement_method: ["Park meetings", "Fuel subsidy pledges", "Accident insurance scheme"],
    talking_points: ["Motorcycle and tricycle ban review", "Operator insurance and accident compensation", "Fuel subsidy and transport cost relief", "Park infrastructure and security"],
    cultural_protocol: "Meet the Park Chairman first — never bypass the hierarchy. Bring drinks and food to the park meeting.",
    office_relevance: { President: 7, Governor: 9, Senator: 7, House: 8, LGA: 10 },
    best_engagement_time: "Midday (slow business hours) or Sunday afternoons",
    key_ask: "Election Day logistics — transporting voters to polling units",
  },
  {
    id: "nut", name: "Nigeria Union of Teachers (NUT) — State Chapter",
    category: "Professional", subcategory: "Education",
    reach_pct: 3.4, priority: 1,
    engagement_method: ["School visits", "Teacher salary pledge", "Classroom infrastructure donation"],
    talking_points: ["Teacher salary arrears payment and reform", "School infrastructure — desks, books, laboratories", "Teacher training and professional development", "Free school meals and uniform programme"],
    cultural_protocol: "Visit schools during school hours. Address the State NUT Chairman. Teachers are opinion leaders in their communities.",
    office_relevance: { President: 8, Governor: 9, Senator: 8, House: 8, LGA: 9 },
    best_engagement_time: "School term periods, Teachers' Day (October 5)",
    key_ask: "Community-level voter education and school-based voter registration",
  },
  {
    id: "nba", name: "Nigerian Bar Association (NBA) — State Branch",
    category: "Professional", subcategory: "Legal",
    reach_pct: 1.2, priority: 2,
    engagement_method: ["Bar dinner sponsorship", "Legal reform debate", "Pro-bono legal aid pledge"],
    talking_points: ["Judicial independence and court funding", "Legal aid for indigent Nigerians", "Anti-corruption court reform", "Electoral law reform and INEC independence"],
    cultural_protocol: "Engage through the Branch Chairman. Lawyers respond to evidence and policy detail — bring a comprehensive manifesto, not slogans.",
    office_relevance: { President: 9, Governor: 8, Senator: 9, House: 7, LGA: 4 },
    best_engagement_time: "Bar association monthly meetings",
    key_ask: "Legal credibility endorsement and election petition monitoring",
  },
  {
    id: "nma", name: "Nigerian Medical Association (NMA) — State Chapter",
    category: "Professional", subcategory: "Healthcare",
    reach_pct: 0.8, priority: 2,
    engagement_method: ["Health summit sponsorship", "Hospital equipment donation", "Medical outreach"],
    talking_points: ["Healthcare funding — 15% of budget (Abuja Declaration)", "Doctor brain drain reversal and salary reform", "Universal health coverage and NHIS expansion", "Primary healthcare centre rehabilitation"],
    cultural_protocol: "Engage through the State Chairman. Doctors are highly respected — treat them as partners, not supporters. Bring data and policy documents.",
    office_relevance: { President: 9, Governor: 9, Senator: 8, House: 7, LGA: 5 },
    best_engagement_time: "Medical association quarterly meetings",
    key_ask: "Professional credibility endorsement and healthcare policy co-design",
  },
  {
    id: "cso", name: "Civil Society Organisations & NGO Networks",
    category: "Civil Society", subcategory: "Advocacy",
    reach_pct: 2.8, priority: 2,
    engagement_method: ["Policy dialogue forums", "Manifesto review sessions", "Transparency pledge signing"],
    talking_points: ["Freedom of information and open government", "Anti-corruption and asset declaration", "Electoral reform and INEC independence", "Human rights and rule of law"],
    cultural_protocol: "CSOs are watchdogs — engage transparently. Sign their candidate pledge forms. Avoid making promises you cannot keep — they will hold you accountable publicly.",
    office_relevance: { President: 9, Governor: 8, Senator: 8, House: 7, LGA: 5 },
    best_engagement_time: "Pre-election policy forums",
    key_ask: "Election monitoring neutrality and policy credibility endorsement",
  },
  {
    id: "diaspora", name: "Nigerian Diaspora Organisations (NDO / NIDCOM Networks)",
    category: "Diaspora", subcategory: "Overseas Nigerians",
    reach_pct: 1.5, priority: 2,
    engagement_method: ["Virtual town halls", "Diaspora remittance policy dialogue", "Investment summit"],
    talking_points: ["Diaspora voting rights and overseas voting", "Dual citizenship and property rights", "Remittance cost reduction and fintech policy", "Investment incentives and diaspora bonds"],
    cultural_protocol: "Engage via Zoom/Teams first. Diaspora Nigerians are highly educated and politically engaged — bring detailed policy documents.",
    office_relevance: { President: 10, Governor: 7, Senator: 7, House: 5, LGA: 3 },
    best_engagement_time: "Weekends (accounting for time zone differences)",
    key_ask: "Campaign fundraising and social media amplification from overseas",
  },
  {
    id: "jonapwd", name: "Joint National Association of Persons with Disabilities (JONAPWD)",
    category: "Inclusion", subcategory: "Disability Rights",
    reach_pct: 2.3, priority: 2,
    engagement_method: ["Accessibility audit pledge", "Assistive device donation", "Inclusive policy commitment"],
    talking_points: ["Disability Rights Act implementation", "Accessible polling unit infrastructure", "Assistive technology and healthcare", "Employment quota for persons with disabilities"],
    cultural_protocol: "Ensure your campaign venue is wheelchair accessible. Use sign language interpreters. Engage the State Chairman directly.",
    office_relevance: { President: 8, Governor: 8, Senator: 7, House: 7, LGA: 6 },
    best_engagement_time: "International Day of Persons with Disabilities (December 3)",
    key_ask: "Inclusive campaign endorsement and accessibility compliance signalling",
  },
];

// Zone-specific additions
const ZONE_ADDITIONS: Record<string, Stakeholder[]> = {
  "South-West": [{
    id: "afenifere", name: "Afenifere (Yoruba Socio-Cultural Organisation)",
    category: "Ethnic/Regional", subcategory: "Yoruba Socio-Political",
    reach_pct: 13.0, priority: 1,
    engagement_method: ["Yoruba summit attendance", "Restructuring policy dialogue", "Obafemi Awolowo legacy alignment"],
    talking_points: ["Restructuring and true federalism", "Yoruba self-determination and security", "Amotekun (regional security) support", "Education and free education legacy"],
    cultural_protocol: "Engage through the Patron. Acknowledge the Awolowo legacy. Dress in aso-oke for formal meetings. Speak Yoruba.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 7 },
    best_engagement_time: "Yoruba Day (July 20) and cultural festivals",
    key_ask: "South-West bloc endorsement — critical for any national office",
  }],
  "South-East": [{
    id: "ohanaeze", name: "Ohanaeze Ndigbo (Igbo Socio-Cultural Organisation)",
    category: "Ethnic/Regional", subcategory: "Igbo Socio-Political",
    reach_pct: 13.5, priority: 1,
    engagement_method: ["Igbo summit attendance", "Restructuring dialogue", "Igbo presidency advocacy"],
    talking_points: ["Igbo presidency and power rotation", "IPOB/ESN security dialogue", "South-East infrastructure (Second Niger Bridge)", "Igbo business and trade protection"],
    cultural_protocol: "Engage through the President-General. Acknowledge the civil war and marginalisation. Speak Igbo phrases. Attend Igbo cultural events.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 7 },
    best_engagement_time: "New Yam Festival (August) and Igbo Day",
    key_ask: "South-East bloc endorsement — critical for any national office",
  }],
  "South-South": [{
    id: "pandef", name: "Pan Niger Delta Forum (PANDEF)",
    category: "Ethnic/Regional", subcategory: "Niger Delta Advocacy",
    reach_pct: 9.0, priority: 1,
    engagement_method: ["Niger Delta summit", "Resource control dialogue", "Environmental justice pledge"],
    talking_points: ["Resource control and fiscal federalism", "NDDC reform and accountability", "Environmental remediation", "Niger Delta security and ex-militant welfare"],
    cultural_protocol: "Engage through the PANDEF leadership. The Niger Delta is politically sophisticated — bring detailed policy positions on resource control.",
    office_relevance: { President: 10, Governor: 9, Senator: 9, House: 7, LGA: 7 },
    best_engagement_time: "Pre-election policy forums",
    key_ask: "South-South bloc endorsement — critical for any national office",
  }],
  "North-West": [{
    id: "arewa", name: "Arewa Consultative Forum (ACF) — State Chapter",
    category: "Ethnic/Regional", subcategory: "Northern Socio-Political",
    reach_pct: 14.0, priority: 1,
    engagement_method: ["Durbar attendance", "Northern development dialogue", "Emirate palace visits"],
    talking_points: ["Northern unity and development", "Security and banditry eradication", "Almajiri reform", "Agricultural investment"],
    cultural_protocol: "Engage through the Emir's palace. Dress in traditional Northern attire (babariga/kaftan). Speak Hausa if possible. Bring kola nuts.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 9 },
    best_engagement_time: "After Friday Jumu'ah prayer",
    key_ask: "Northern bloc endorsement — critical for any national office",
  }],
  "North-Central": [{
    id: "middlebelt", name: "Middle Belt Forum (MBF)",
    category: "Ethnic/Regional", subcategory: "Middle Belt Socio-Political",
    reach_pct: 11.0, priority: 1,
    engagement_method: ["Middle Belt summit", "Farmer-herder resolution pledge", "Minority rights dialogue"],
    talking_points: ["Middle Belt security and farmer protection", "Minority rights and federal character", "Land rights and anti-grazing law", "Rural development"],
    cultural_protocol: "Engage through the Forum President. Acknowledge the historical marginalisation of the Middle Belt. Avoid taking sides on farmer-herder conflict publicly.",
    office_relevance: { President: 10, Governor: 9, Senator: 9, House: 8, LGA: 8 },
    best_engagement_time: "Post-harvest season when security is relatively stable",
    key_ask: "Middle Belt bloc endorsement — a swing region in national elections",
  }],
  "North-East": [{
    id: "borno_elders", name: "Borno Elders Forum & Lake Chad Basin Communities",
    category: "Traditional Leaders", subcategory: "Conflict-Affected Communities",
    reach_pct: 8.0, priority: 1,
    engagement_method: ["IDP camp visits", "Reconstruction pledge", "Security dialogue"],
    talking_points: ["Boko Haram/ISWAP eradication", "IDP return and resettlement", "Reconstruction of destroyed communities", "Lake Chad restoration"],
    cultural_protocol: "Visit IDP camps personally. Show empathy before policy. Engage the Shehu of Borno's palace.",
    office_relevance: { President: 10, Governor: 10, Senator: 9, House: 8, LGA: 9 },
    best_engagement_time: "Dry season (safer for travel)",
    key_ask: "Conflict-affected community mobilisation and security credibility",
  }],
};

// ── Scoring function ──────────────────────────────────────────────────────────
function scoreStakeholders(
  stateCode: string,
  office: string,
  religion: string,
  gender: string,
): Stakeholder[] {
  const state = STATES.find(s => s.code === stateCode);
  const zone = state?.zone ?? "South-West";
  const zoneExtras = ZONE_ADDITIONS[zone] ?? [];
  const pool = [...zoneExtras, ...ALL_STAKEHOLDERS];

  return pool
    .map(s => {
      const baseScore = (s.office_relevance[office] ?? 5) * 10;
      const reachScore = Math.log1p(s.reach_pct) * 10;
      const religionBonus =
        (religion === "Muslim" && s.subcategory === "Islamic") ||
        (religion === "Christian" && s.subcategory.startsWith("Christian"))
          ? 15 : 0;
      const genderBonus = gender === "Female" && s.category === "Women" ? 20 : 0;
      const priorityWeight = s.priority === 1 ? 10 : 0;
      const totalScore = baseScore + reachScore + religionBonus + genderBonus + priorityWeight;
      const pop = 3_000_000; // approximate
      return {
        ...s,
        relevance_score: Math.round(totalScore * 10) / 10,
        estimated_voter_reach: Math.round(pop * s.reach_pct / 100),
      };
    })
    .sort((a, b) => (b.relevance_score ?? 0) - (a.relevance_score ?? 0));
}

// ── Component ─────────────────────────────────────────────────────────────────
export default function StakeholdersPage() {
  const [candidateName, setCandidateName] = useState("Aminu Bello");
  const [stateCode, setStateCode] = useState("LAG");
  const [office, setOffice] = useState("Governor");
  const [religion, setReligion] = useState("Mixed");
  const [gender, setGender] = useState("Male");
  const [partyName, setPartyName] = useState("");
  const [partyColor, setPartyColor] = useState("#006400");
  const [partyLogo, setPartyLogo] = useState("");
  const [filterCategory, setFilterCategory] = useState("All");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedStakeholder, setSelectedStakeholder] = useState<Stakeholder | null>(null);
  const [generated, setGenerated] = useState(false);
  const [loading, setLoading] = useState(false);
  const [results, setResults] = useState<Stakeholder[]>([]);
  const [activeTab, setActiveTab] = useState<"recommendations" | "calendar" | "lga" | "crm" | "dashboard" | "network" | "compare">("recommendations");
  const [crmContacts, setCrmContacts] = useState<CRMContact[]>([]);
  const { permission, scheduleReminder, cancelReminder, hasReminder, requestPermission } = useNotificationReminders();

  // ── WhatsApp Quick-Share ───────────────────────────────────────────────────────
  function shareOnWhatsApp(s: Stakeholder) {
    const lines = [
      `📋 *STAKEHOLDER BRIEF — ${s.name}*`,
      `Category: ${s.category} (${s.subcategory})`,
      `Est. Reach: ~${((s.estimated_voter_reach ?? 0) / 1000).toFixed(0)}K voters`,
      ``,
      `*Key Ask:* ${s.key_ask}`,
      ``,
      `*Talking Points:*`,
      ...s.talking_points.map((tp: string, i: number) => `${i + 1}. ${tp}`),
      ``,
      `*Cultural Protocol:* ${s.cultural_protocol}`,
      ``,
      `*Best Engagement Time:* ${s.best_engagement_time}`,
      ``,
      `_Engagement Methods: ${s.engagement_method.join(", ")}_`,
      ``,
      `— INEC Campaign Intelligence Engine`,
    ];
    const url = `https://wa.me/?text=${encodeURIComponent(lines.join("\n"))}`;
    window.open(url, "_blank", "noopener,noreferrer");
  }

  const selectedState = STATES.find(s => s.code === stateCode);

  const categories = useMemo(() => {
    const cats = new Set(results.map(s => s.category));
    return ["All", ...Array.from(cats)];
  }, [results]);

  const filtered = useMemo(() => {
    return results.filter(s => {
      const matchCat = filterCategory === "All" || s.category === filterCategory;
      const matchSearch = searchQuery === "" ||
        s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        s.category.toLowerCase().includes(searchQuery.toLowerCase()) ||
        s.key_ask.toLowerCase().includes(searchQuery.toLowerCase());
      return matchCat && matchSearch;
    });
  }, [results, filterCategory, searchQuery]);

  function handleGenerate() {
    // Persist branding to localStorage for EndorsementTracker page
    if (partyLogo) localStorage.setItem("inec_party_logo", partyLogo);
    if (partyColor) localStorage.setItem("inec_party_color", partyColor);
    if (partyName) localStorage.setItem("inec_party_name", partyName);
    localStorage.setItem("inec_candidate_name", candidateName);
    setLoading(true);
    setSelectedStakeholder(null);
    setTimeout(() => {
      const scored = scoreStakeholders(stateCode, office, religion, gender);
      setResults(scored);
      setGenerated(true);
      setLoading(false);
      setFilterCategory("All");
    }, 900);
  }

  const totalReach = results.reduce((acc, s) => acc + (s.estimated_voter_reach ?? 0), 0);
  const priority1Count = results.filter(s => s.priority === 1).length;

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "oklch(0.13 0.008 240)", color: "oklch(0.88 0.005 240)", fontFamily: "'IBM Plex Mono', monospace" }}>
      {/* Header */}
      <header className="border-b flex items-center justify-between px-6 py-3" style={{ borderColor: "oklch(0.25 0.01 240)", background: "oklch(0.11 0.008 240)" }}>
        <div className="flex items-center gap-3">
          <div className="w-7 h-7 rounded flex items-center justify-center" style={{ background: "oklch(0.55 0.18 145)" }}>
            <Users className="w-4 h-4 text-white" />
          </div>
          <div>
            <div className="text-sm font-bold tracking-widest uppercase" style={{ color: "oklch(0.88 0.005 240)" }}>INEC Campaign Intelligence</div>
            <div className="text-xs tracking-wider" style={{ color: "oklch(0.55 0.01 240)" }}>STAKEHOLDER ENGAGEMENT ENGINE · v5.0</div>
          </div>
        </div>
        <div className="flex items-center gap-6 text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
          <span>36 STATES + FCT</span>
          <span>·</span>
          <span>28 STAKEHOLDER GROUPS</span>
          <span>·</span>
          <span>90-DAY CALENDAR</span>
          <span>·</span>
          <span>LGA DRILL-DOWN</span>
          <span>·</span>
          <span>CRM</span>
          <span>·</span>
          <span>NETWORK</span>
          <span>·</span>
          <span>COMPARE</span>
          <span>·</span>
          <a
            href="/endorsements"
            className="text-xs tracking-wider transition-all"
            style={{ color: "oklch(0.65 0.18 145)", textDecoration: "none" }}
          >
            ENDORSEMENTS ↗
          </a>
          <span>·</span>
          <span className="flex items-center gap-1" style={{ color: "oklch(0.65 0.18 145)" }}>
            <span className="w-1.5 h-1.5 rounded-full inline-block" style={{ background: "oklch(0.65 0.18 145)" }} />
            LIVE
          </span>
        </div>
      </header>

      <div className="flex flex-1">
        {/* Left Sidebar — Candidate Profile */}
        <aside className="w-72 flex-shrink-0 border-r flex flex-col" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.115 0.008 240)" }}>
          <div className="p-4 border-b" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
            <div className="text-xs font-bold tracking-widest uppercase mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>Candidate Profile</div>

            <div className="space-y-3">
              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>CANDIDATE NAME</label>
                <input
                  value={candidateName}
                  onChange={e => setCandidateName(e.target.value)}
                  className="w-full px-2 py-1.5 text-sm rounded border"
                  style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
                />
              </div>

              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>STATE</label>
                <select
                  value={stateCode}
                  onChange={e => setStateCode(e.target.value)}
                  className="w-full px-2 py-1.5 text-sm rounded border"
                  style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
                >
                  {STATES.map(s => <option key={s.code} value={s.code}>{s.name}</option>)}
                </select>
              </div>

              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>OFFICE SOUGHT</label>
                <div className="grid grid-cols-3 gap-1">
                  {OFFICES.map(o => (
                    <button
                      key={o}
                      onClick={() => setOffice(o)}
                      className="py-1 text-xs rounded border transition-all"
                      style={{
                        background: office === o ? "oklch(0.55 0.18 145)" : "oklch(0.18 0.008 240)",
                        borderColor: office === o ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)",
                        color: office === o ? "white" : "oklch(0.65 0.01 240)",
                      }}
                    >{o}</button>
                  ))}
                </div>
              </div>

              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>RELIGION</label>
                <select
                  value={religion}
                  onChange={e => setReligion(e.target.value)}
                  className="w-full px-2 py-1.5 text-sm rounded border"
                  style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
                >
                  <option value="Mixed">Mixed / Prefer not to say</option>
                  <option value="Christian">Christian</option>
                  <option value="Muslim">Muslim</option>
                  <option value="Traditional">Traditional</option>
                </select>
              </div>

              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>GENDER</label>
                <div className="grid grid-cols-2 gap-1">
                  {["Male", "Female"].map(g => (
                    <button
                      key={g}
                      onClick={() => setGender(g)}
                      className="py-1 text-xs rounded border transition-all"
                      style={{
                        background: gender === g ? "oklch(0.45 0.18 280)" : "oklch(0.18 0.008 240)",
                        borderColor: gender === g ? "oklch(0.45 0.18 280)" : "oklch(0.28 0.01 240)",
                        color: gender === g ? "white" : "oklch(0.65 0.01 240)",
                      }}
                    >{g}</button>
                  ))}
                </div>
              </div>
            </div>
          </div>

          {/* Party Branding */}
          <div className="p-4 border-b" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
            <div className="text-xs font-bold tracking-widest uppercase mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>Party Branding</div>
            <div className="space-y-2">
              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>PARTY NAME</label>
                <input
                  value={partyName}
                  onChange={e => setPartyName(e.target.value)}
                  placeholder="e.g. APC, PDP, LP"
                  className="w-full px-2 py-1.5 text-sm rounded border"
                  style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
                />
              </div>
              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>PARTY COLOUR</label>
                <div className="flex items-center gap-2">
                  <input
                    type="color"
                    value={partyColor}
                    onChange={e => setPartyColor(e.target.value)}
                    className="w-8 h-7 rounded border cursor-pointer"
                    style={{ borderColor: "oklch(0.28 0.01 240)", background: "oklch(0.18 0.008 240)", padding: "1px" }}
                  />
                  <span className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{partyColor}</span>
                </div>
              </div>
              <div>
                <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>PARTY LOGO</label>
                <label className="flex items-center gap-2 px-2 py-1.5 text-xs rounded border cursor-pointer transition-all"
                  style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}>
                  <input
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={e => {
                      const file = e.target.files?.[0];
                      if (!file) return;
                      const reader = new FileReader();
                      reader.onload = ev => setPartyLogo(ev.target?.result as string ?? "");
                      reader.readAsDataURL(file);
                    }}
                  />
                  {partyLogo ? (
                    <><img src={partyLogo} alt="logo" className="h-5 w-5 object-contain rounded" /><span>Change Logo</span></>
                  ) : (
                    <span>Upload Logo (PNG/SVG)</span>
                  )}
                </label>
              </div>
            </div>
          </div>
          {/* Zone info */}
          {selectedState && (
            <div className="p-4 border-b" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
              <div className="text-xs tracking-wider mb-2" style={{ color: "oklch(0.55 0.01 240)" }}>GEOPOLITICAL ZONE</div>
              <div className="text-sm font-bold" style={{ color: "oklch(0.75 0.15 145)" }}>{selectedState.zone}</div>
              <div className="text-xs mt-1" style={{ color: "oklch(0.55 0.01 240)" }}>Zone-specific stakeholders will be prioritised</div>
            </div>
          )}

          {/* Generate button */}
          <div className="p-4">
            <button
              onClick={handleGenerate}
              disabled={loading || !candidateName.trim()}
              className="w-full py-2.5 text-sm font-bold tracking-widest uppercase rounded flex items-center justify-center gap-2 transition-all"
              style={{
                background: loading ? "oklch(0.35 0.1 145)" : "oklch(0.55 0.18 145)",
                color: "white",
                opacity: !candidateName.trim() ? 0.5 : 1,
              }}
            >
              {loading ? (
                <>
                  <div className="w-3 h-3 border border-white/40 border-t-white rounded-full animate-spin" />
                  ANALYSING...
                </>
              ) : (
                <>
                  <Users className="w-4 h-4" />
                  GENERATE PLAN
                </>
              )}
            </button>
          </div>

          {/* Stats */}
          {generated && !loading && (
            <div className="p-4 border-t space-y-3" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
              <div className="text-xs tracking-wider mb-2" style={{ color: "oklch(0.55 0.01 240)" }}>PLAN SUMMARY</div>
              <div className="flex justify-between text-xs">
                <span style={{ color: "oklch(0.55 0.01 240)" }}>Groups Identified</span>
                <span className="font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>{results.length}</span>
              </div>
              <div className="flex justify-between text-xs">
                <span style={{ color: "oklch(0.55 0.01 240)" }}>Priority 1 Groups</span>
                <span className="font-bold" style={{ color: "oklch(0.65 0.18 145)" }}>{priority1Count}</span>
              </div>
              <div className="flex justify-between text-xs">
                <span style={{ color: "oklch(0.55 0.01 240)" }}>Est. Total Reach</span>
                <span className="font-bold" style={{ color: "oklch(0.75 0.15 50)" }}>{(totalReach / 1_000_000).toFixed(1)}M</span>
              </div>
            </div>
          )}
          {/* Sentiment Feed — live approval tracker */}
          {generated && !loading && (
            <div className="p-4 border-t" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
              <SentimentFeed
                candidateName={candidateName}
                office={office}
                stateName={selectedState?.name ?? stateCode}
                compact
              />
            </div>
          )}
          {/* Campaign Tools Quick Access */}
          <div className="p-4 border-t overflow-y-auto" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
            <div className="text-xs font-bold tracking-widest uppercase mb-2" style={{ color: "oklch(0.55 0.01 240)" }}>Campaign Tools</div>
            <div className="space-y-0.5">
              {[
                { label: "Campaign Timeline", href: "/timeline" },
                { label: "Voter Registration", href: "/registration" },
                { label: "Polling Unit Locator", href: "/polling-units" },
                { label: "Volunteer Portal", href: "/volunteers" },
                { label: "Press Release Generator", href: "/press-release" },
                { label: "Social Media Center", href: "/social-media" },
                { label: "Legal Compliance", href: "/legal-compliance" },
                { label: "Opposition Research", href: "/opposition-research" },
                { label: "Election Day War Room", href: "/war-room" },
                { label: "Results Projection", href: "/results" },
                { label: "Manifesto Builder", href: "/manifesto" },
                { label: "Petition Drive", href: "/petition" },
                { label: "Diaspora Outreach", href: "/diaspora" },
                { label: "Post-Election Analytics", href: "/post-election" },
                { label: "Candidate Website", href: "/candidate-website" },
                { label: "Media Monitoring", href: "/media-monitoring" },
                { label: "Debate Coach", href: "/debate-coach" },
                { label: "Fundraising Tracker", href: "/fundraising" },
                { label: "Budget Planner", href: "/budget" },
                { label: "Endorsement Tracker", href: "/endorsements" },
              ].map(tool => (
                <a key={tool.href} href={tool.href} className="flex items-center justify-between text-xs px-2 py-1.5 rounded hover:bg-white/5 transition-colors" style={{ color: "oklch(0.60 0.01 240)", textDecoration: "none" }}>
                  <span>{tool.label}</span>
                  <span style={{ color: "oklch(0.35 0.01 240)" }}>↗</span>
                </a>
              ))}
            </div>
          </div>
        </aside>

        {/* Main Content */}
        <main className="flex-1 flex flex-col overflow-hidden">
          {!generated && !loading && (
            <div className="flex-1 flex flex-col items-center justify-center gap-4" style={{ color: "oklch(0.45 0.01 240)" }}>
              <Users className="w-12 h-12 opacity-30" />
              <div className="text-center">
                <div className="text-sm font-bold mb-1">Stakeholder Engagement Engine</div>
                <div className="text-xs">Configure your candidate profile and click Generate Plan</div>
                <div className="text-xs mt-1">Covers 28 stakeholder groups across all 36 states + FCT</div>
                <div className="text-xs mt-1" style={{ color: "oklch(0.55 0.18 145)" }}>+ 90-Day Calendar · LGA Drill-Down · Contact CRM</div>
              </div>
            </div>
          )}

          {loading && (
            <div className="flex-1 flex flex-col items-center justify-center gap-3">
              <div className="w-8 h-8 border-2 rounded-full animate-spin" style={{ borderColor: "oklch(0.3 0.01 240)", borderTopColor: "oklch(0.55 0.18 145)" }} />
              <div className="text-xs tracking-widest" style={{ color: "oklch(0.55 0.01 240)" }}>SCORING {results.length || 28} STAKEHOLDER GROUPS...</div>
            </div>
          )}

          {generated && !loading && (
            <>
              {/* Tab navigation */}
              <div className="flex items-center gap-0 border-b px-4 overflow-x-auto" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.115 0.008 240)" }}>
                {([
                  { id: "recommendations", label: "Recommendations", count: results.length },
                  { id: "calendar",        label: "90-Day Calendar", count: null },
                  { id: "lga",             label: "LGA Drill-Down",  count: null },
                  { id: "crm",             label: "Contact CRM",     count: null },
                  { id: "dashboard",       label: "Dashboard",       count: null },
                  { id: "network",         label: "Network Graph",   count: null },
                  { id: "compare",         label: "Compare",         count: null },
                ] as { id: string; label: string; count: number | null }[]).map(tab => (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id as "recommendations" | "calendar" | "lga" | "crm" | "dashboard" | "network" | "compare")}
                    className="flex items-center gap-1.5 px-4 py-3 text-xs border-b-2 transition-all"
                    style={{
                      borderBottomColor: activeTab === tab.id ? "oklch(0.55 0.18 145)" : "transparent",
                      color: activeTab === tab.id ? "oklch(0.88 0.005 240)" : "oklch(0.45 0.01 240)",
                    }}
                  >
                    {tab.label}
                    {tab.count !== null && (
                      <span className="px-1.5 py-0.5 rounded text-xs" style={{ background: "oklch(0.22 0.01 240)", color: "oklch(0.65 0.01 240)" }}>
                        {tab.count}
                      </span>
                    )}
                  </button>
                ))}
                <div className="ml-auto flex-shrink-0 flex items-center px-3 border-l" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
                  <StakeholderBriefPDF
                    stakeholders={results}
                    candidateName={candidateName}
                    office={office}
                    stateName={selectedState?.name ?? stateCode}
                    party={partyName}
                    partyLogo={partyLogo}
                    partyColor={partyColor}
                  />
                </div>
              </div>

              {/* Tab: Recommendations */}
              {activeTab === "recommendations" && (
                <>
                <div className="flex items-center gap-3 px-4 py-2 border-b" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
                  <div className="flex items-center gap-2 flex-1 px-3 py-1.5 rounded border" style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)" }}>
                    <Search className="w-3.5 h-3.5" style={{ color: "oklch(0.55 0.01 240)" }} />
                    <input
                      value={searchQuery}
                      onChange={e => setSearchQuery(e.target.value)}
                      placeholder="Search stakeholders..."
                      className="flex-1 text-xs bg-transparent outline-none"
                      style={{ color: "oklch(0.88 0.005 240)" }}
                    />
                  </div>
                  <div className="flex items-center gap-1 flex-wrap">
                    <Filter className="w-3.5 h-3.5 mr-1" style={{ color: "oklch(0.55 0.01 240)" }} />
                    {categories.map(cat => (
                      <button
                        key={cat}
                        onClick={() => setFilterCategory(cat)}
                        className="px-2 py-1 text-xs rounded transition-all"
                        style={{
                          background: filterCategory === cat ? "oklch(0.55 0.18 145)" : "oklch(0.18 0.008 240)",
                          color: filterCategory === cat ? "white" : "oklch(0.55 0.01 240)",
                          border: `1px solid ${filterCategory === cat ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)"}`,
                        }}
                      >{cat}</button>
                    ))}
                  </div>
                </div>

                <div className="px-4 py-2 border-b flex items-center justify-between" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
                  <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
                    {office} · {selectedState?.name} · {selectedState?.zone}
                  </div>
                  <div className="flex items-center gap-2 text-xs px-3 py-1 rounded" style={{ background: "oklch(0.18 0.008 240)", border: "1px solid oklch(0.28 0.01 240)" }}>
                    <CheckCircle2 className="w-3 h-3" style={{ color: "oklch(0.65 0.18 145)" }} />
                    <span style={{ color: "oklch(0.65 0.18 145)" }}>{filtered.length} groups · {(totalReach / 1_000_000).toFixed(1)}M est. reach</span>
                  </div>
                </div>

                {/* Stakeholder grid */}
                <div className="flex-1 overflow-y-auto p-4">
                  <div className="grid grid-cols-2 gap-3">
                    <AnimatePresence>
                      {filtered.map((s, i) => (
                        <motion.div
                          key={s.id}
                          initial={{ opacity: 0, y: 10 }}
                          animate={{ opacity: 1, y: 0 }}
                          transition={{ delay: i * 0.03 }}
                          onClick={() => setSelectedStakeholder(selectedStakeholder?.id === s.id ? null : s)}
                          className="rounded border cursor-pointer transition-all"
                          style={{
                            background: selectedStakeholder?.id === s.id ? "oklch(0.18 0.012 240)" : "oklch(0.155 0.008 240)",
                            borderColor: selectedStakeholder?.id === s.id ? "oklch(0.55 0.18 145)" : "oklch(0.22 0.01 240)",
                          }}
                        >
                          <div className="p-3">
                            <div className="flex items-start justify-between gap-2 mb-2">
                              <div className="flex items-center gap-2">
                                {s.priority === 1 && (
                                  <Star className="w-3 h-3 flex-shrink-0" style={{ color: "oklch(0.75 0.18 80)" }} fill="oklch(0.75 0.18 80)" />
                                )}
                                <span className="text-xs font-bold leading-tight" style={{ color: "oklch(0.88 0.005 240)" }}>{s.name}</span>
                              </div>
                              <span className="text-xs font-mono flex-shrink-0" style={{ color: "oklch(0.65 0.18 145)" }}>
                                {s.reach_pct.toFixed(1)}%
                              </span>
                    </div>

                            <div className="flex items-center gap-2 mb-2">
                              <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs border ${CATEGORY_COLORS[s.category] ?? "bg-gray-900/40 text-gray-300 border-gray-700/50"}`}>
                                {CATEGORY_ICONS[s.category]}
                                {s.category}
                              </span>
                              <span className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>{s.subcategory}</span>
                            </div>

                            <div className="text-xs leading-relaxed" style={{ color: "oklch(0.65 0.01 240)" }}>
                              <span className="font-bold" style={{ color: "oklch(0.55 0.18 145)" }}>Key Ask: </span>
                              {s.key_ask}
                            </div>

                            <div className="flex items-center justify-between mt-2">
                              <span className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                                ~{((s.estimated_voter_reach ?? 0) / 1000).toFixed(0)}K voters
                              </span>
                              <div className="flex items-center gap-1 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                                <span>Score: {s.relevance_score?.toFixed(0)}</span>
                                <ChevronRight className="w-3 h-3" />
                              </div>
                            </div>
                          </div>

                          {/* Expanded detail */}
                          <AnimatePresence>
                            {selectedStakeholder?.id === s.id && (
                              <motion.div
                                initial={{ height: 0, opacity: 0 }}
                                animate={{ height: "auto", opacity: 1 }}
                                exit={{ height: 0, opacity: 0 }}
                                className="overflow-hidden"
                              >
                                <div className="px-3 pb-3 border-t space-y-3" style={{ borderColor: "oklch(0.25 0.01 240)" }}>
                                  <div className="pt-3">
                                    <div className="text-xs font-bold tracking-wider mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>ENGAGEMENT METHODS</div>
                                    <div className="flex flex-wrap gap-1">
                                      {s.engagement_method.map(m => (
                                        <span key={m} className="text-xs px-1.5 py-0.5 rounded" style={{ background: "oklch(0.22 0.01 240)", color: "oklch(0.75 0.01 240)" }}>{m}</span>
                                      ))}
                                    </div>
                                  </div>
                                  <div>
                                    <div className="text-xs font-bold tracking-wider mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>TALKING POINTS</div>
                                    <ul className="space-y-0.5">
                                      {s.talking_points.map(tp => (
                                        <li key={tp} className="flex items-start gap-1.5 text-xs" style={{ color: "oklch(0.72 0.01 240)" }}>
                                          <ArrowRight className="w-3 h-3 flex-shrink-0 mt-0.5" style={{ color: "oklch(0.55 0.18 145)" }} />
                                          {tp}
                                        </li>
                                      ))}
                                    </ul>
                                  </div>
                                  <div className="flex items-start gap-2 p-2 rounded" style={{ background: "oklch(0.18 0.012 240)", border: "1px solid oklch(0.28 0.01 240)" }}>
                                    <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" style={{ color: "oklch(0.75 0.18 80)" }} />
                                    <div>
                                      <div className="text-xs font-bold mb-0.5" style={{ color: "oklch(0.75 0.18 80)" }}>Cultural Protocol</div>
                                      <div className="text-xs" style={{ color: "oklch(0.65 0.01 240)" }}>{s.cultural_protocol}</div>
                                    </div>
                                  </div>
                                  <div className="flex items-center gap-2 text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
                                    <Info className="w-3 h-3" />
                                    <span>Best time: {s.best_engagement_time}</span>
                                  </div>
                                  {/* WhatsApp Quick-Share */}
                                  <button
                                    onClick={(e) => { e.stopPropagation(); shareOnWhatsApp(s); }}
                                    className="flex items-center gap-2 w-full px-3 py-2 rounded text-xs font-bold transition-all"
                                    style={{ background: "oklch(0.32 0.12 145)", color: "white", border: "1px solid oklch(0.45 0.15 145)" }}
                                  >
                                    <MessageCircle className="w-3.5 h-3.5" />
                                    Share Brief via WhatsApp
                                  </button>
                                </div>
                              </motion.div>
                            )}
                          </AnimatePresence>
                        </motion.div>
                      ))}
                    </AnimatePresence>
                  </div>
                </div>
                </>
              )}

              {/* Tab: Dashboard */}
              {activeTab === "dashboard" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <EngagementDashboard
                    contacts={crmContacts}
                    stakeholders={results}
                    candidateName={candidateName}
                    office={office}
                    stateName={selectedState?.name ?? stateCode}
                  />
                </div>
              )}

              {/* Tab: Network Graph */}
              {activeTab === "network" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <div className="mb-3">
                    <h3 className="text-sm font-bold mb-1" style={{ color: "oklch(0.88 0.005 240)" }}>Stakeholder Influence Network</h3>
                    <p className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
                      Force-directed graph showing coalition pathways, referral relationships, and overlap between stakeholder groups.
                      Node size reflects priority score. Hover a node for details.
                    </p>
                  </div>
                  <StakeholderNetworkGraph stakeholders={results} />
                </div>
              )}

              {/* Tab: Compare */}
              {activeTab === "compare" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <div className="mb-3">
                    <h3 className="text-sm font-bold mb-1" style={{ color: "oklch(0.88 0.005 240)" }}>Multi-Candidate Comparison</h3>
                    <p className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
                      Configure a rival candidate profile to see stakeholder overlap, contested groups, and strategic recommendations.
                    </p>
                  </div>
                  <CandidateComparison
                    primaryStakeholders={results}
                    primaryName={candidateName}
                    primaryOffice={office}
                    stateName={selectedState?.name ?? stateCode}
                  />
                </div>
              )}

              {/* Tab: 90-Day Calendar */}
              {activeTab === "calendar" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <EngagementCalendar
                    stakeholders={results}
                    candidateName={candidateName}
                    stateName={selectedState?.name ?? stateCode}
                    office={office}
                    scheduleReminder={scheduleReminder}
                    cancelReminder={cancelReminder}
                    hasReminder={hasReminder}
                    notificationPermission={permission}
                    onRequestPermission={requestPermission}
                  />
                </div>
              )}

              {/* Tab: LGA Drill-Down */}
              {activeTab === "lga" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <LGADrillDown
                    stateCode={stateCode}
                    stateName={selectedState?.name ?? stateCode}
                  />
                </div>
              )}

              {/* Tab: Contact CRM */}
              {activeTab === "crm" && (
                <div className="flex-1 overflow-y-auto p-4">
                  <StakeholderCRM
                    stakeholders={results}
                    onContactsChange={setCrmContacts}
                  />
                </div>
              )}
            </>
          )}
        </main>
      </div>
    </div>
  );
}
