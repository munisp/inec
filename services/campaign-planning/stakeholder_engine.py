"""
INEC Campaign Planning — Stakeholder Recommendation Engine
===========================================================
Comprehensive database of local stakeholders, youth groups, women associations,
market unions, religious bodies, traditional rulers, civil society organisations,
and professional associations for all 36 Nigerian states + FCT.

For each state the engine returns:
  - Prioritised list of stakeholder groups with engagement strategies
  - Office-level relevance scores (President, Governor, Senator, House, LGA)
  - Recommended engagement sequence and talking points
  - Estimated voter reach per group
  - Cultural protocols and etiquette notes
"""

from __future__ import annotations
from typing import Dict, List, Optional
import math

# ─── Geopolitical Zone Definitions ───────────────────────────────────────────
ZONES: Dict[str, List[str]] = {
    "North-Central": ["KOG", "BNU", "NAS", "KWA", "PLT", "NGR", "FCT"],
    "North-East":    ["ADA", "BOR", "GOM", "TAR", "YOB", "BAU"],
    "North-West":    ["KAN", "KAT", "KBB", "KEB", "SOK", "ZAM", "JIG"],
    "South-East":    ["ABI", "ANM", "EBO", "ENU", "IMO"],
    "South-South":   ["AKW", "BAY", "CRS", "DEL", "EDO", "RIV"],
    "South-West":    ["EKI", "LAG", "OGU", "OND", "OSU", "OYO"],
}

STATE_META: Dict[str, Dict] = {
    "FCT": {"name": "FCT — Abuja",         "zone": "North-Central", "pop": 3_675_000, "lgas": 6,  "dominant_religion": "Mixed",    "major_ethnic": ["Gbagyi", "Hausa", "Yoruba", "Igbo"]},
    "ABI": {"name": "Abia",                 "zone": "South-East",    "pop": 3_727_000, "lgas": 17, "dominant_religion": "Christian","major_ethnic": ["Igbo"]},
    "ADA": {"name": "Adamawa",              "zone": "North-East",    "pop": 4_243_000, "lgas": 21, "dominant_religion": "Mixed",    "major_ethnic": ["Fulani", "Bachama", "Kilba"]},
    "AKW": {"name": "Akwa Ibom",            "zone": "South-South",   "pop": 5_450_000, "lgas": 31, "dominant_religion": "Christian","major_ethnic": ["Ibibio", "Annang", "Oron"]},
    "ANM": {"name": "Anambra",              "zone": "South-East",    "pop": 5_527_000, "lgas": 21, "dominant_religion": "Christian","major_ethnic": ["Igbo"]},
    "BAU": {"name": "Bauchi",               "zone": "North-East",    "pop": 6_537_000, "lgas": 20, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani", "Bolewa"]},
    "BAY": {"name": "Bayelsa",              "zone": "South-South",   "pop": 2_278_000, "lgas": 8,  "dominant_religion": "Christian","major_ethnic": ["Ijaw"]},
    "BNU": {"name": "Benue",                "zone": "North-Central", "pop": 5_742_000, "lgas": 23, "dominant_religion": "Christian","major_ethnic": ["Tiv", "Idoma", "Igede"]},
    "BOR": {"name": "Borno",                "zone": "North-East",    "pop": 5_860_000, "lgas": 27, "dominant_religion": "Muslim",   "major_ethnic": ["Kanuri", "Shuwa Arab", "Fulani"]},
    "CRS": {"name": "Cross River",          "zone": "South-South",   "pop": 3_737_000, "lgas": 18, "dominant_religion": "Christian","major_ethnic": ["Efik", "Ejagham", "Bekwarra"]},
    "DEL": {"name": "Delta",                "zone": "South-South",   "pop": 5_663_000, "lgas": 25, "dominant_religion": "Christian","major_ethnic": ["Urhobo", "Ijaw", "Itsekiri", "Isoko"]},
    "EBO": {"name": "Ebonyi",               "zone": "South-East",    "pop": 2_880_000, "lgas": 13, "dominant_religion": "Christian","major_ethnic": ["Igbo"]},
    "EDO": {"name": "Edo",                  "zone": "South-South",   "pop": 4_736_000, "lgas": 18, "dominant_religion": "Christian","major_ethnic": ["Edo", "Esan", "Owan"]},
    "EKI": {"name": "Ekiti",                "zone": "South-West",    "pop": 3_270_000, "lgas": 16, "dominant_religion": "Christian","major_ethnic": ["Yoruba"]},
    "ENU": {"name": "Enugu",                "zone": "South-East",    "pop": 4_411_000, "lgas": 17, "dominant_religion": "Christian","major_ethnic": ["Igbo"]},
    "GOM": {"name": "Gombe",                "zone": "North-East",    "pop": 3_256_000, "lgas": 11, "dominant_religion": "Muslim",   "major_ethnic": ["Tangale", "Waja", "Fulani"]},
    "IMO": {"name": "Imo",                  "zone": "South-East",    "pop": 5_408_000, "lgas": 27, "dominant_religion": "Christian","major_ethnic": ["Igbo"]},
    "JIG": {"name": "Jigawa",               "zone": "North-West",    "pop": 5_828_000, "lgas": 27, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani"]},
    "KAN": {"name": "Kano",                 "zone": "North-West",    "pop": 13_076_000,"lgas": 44, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani"]},
    "KAT": {"name": "Katsina",              "zone": "North-West",    "pop": 7_831_000, "lgas": 34, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani"]},
    "KBB": {"name": "Kebbi",                "zone": "North-West",    "pop": 4_440_000, "lgas": 21, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani", "Kambari"]},
    "KEB": {"name": "Kogi",                 "zone": "North-Central", "pop": 4_473_000, "lgas": 21, "dominant_religion": "Mixed",    "major_ethnic": ["Igala", "Ebira", "Okun"]},
    "KOG": {"name": "Kwara",                "zone": "North-Central", "pop": 3_193_000, "lgas": 16, "dominant_religion": "Mixed",    "major_ethnic": ["Yoruba", "Nupe", "Bariba"]},
    "KWA": {"name": "Kwara",                "zone": "North-Central", "pop": 3_193_000, "lgas": 16, "dominant_religion": "Mixed",    "major_ethnic": ["Yoruba", "Nupe", "Bariba"]},
    "LAG": {"name": "Lagos",                "zone": "South-West",    "pop": 14_862_000,"lgas": 20, "dominant_religion": "Mixed",    "major_ethnic": ["Yoruba", "Igbo", "Hausa"]},
    "NAS": {"name": "Nasarawa",             "zone": "North-Central", "pop": 2_523_000, "lgas": 13, "dominant_religion": "Mixed",    "major_ethnic": ["Eggon", "Tiv", "Hausa"]},
    "NGR": {"name": "Niger",                "zone": "North-Central", "pop": 5_558_000, "lgas": 25, "dominant_religion": "Muslim",   "major_ethnic": ["Nupe", "Gbagyi", "Hausa"]},
    "OGU": {"name": "Ogun",                 "zone": "South-West",    "pop": 5_217_000, "lgas": 20, "dominant_religion": "Christian","major_ethnic": ["Yoruba"]},
    "OND": {"name": "Ondo",                 "zone": "South-West",    "pop": 4_112_000, "lgas": 18, "dominant_religion": "Christian","major_ethnic": ["Yoruba", "Ijaw"]},
    "OSU": {"name": "Osun",                 "zone": "South-West",    "pop": 4_705_000, "lgas": 30, "dominant_religion": "Christian","major_ethnic": ["Yoruba"]},
    "OYO": {"name": "Oyo",                  "zone": "South-West",    "pop": 7_840_000, "lgas": 33, "dominant_religion": "Mixed",    "major_ethnic": ["Yoruba"]},
    "PLT": {"name": "Plateau",              "zone": "North-Central", "pop": 4_200_000, "lgas": 17, "dominant_religion": "Christian","major_ethnic": ["Berom", "Anaguta", "Afizere"]},
    "RIV": {"name": "Rivers",               "zone": "South-South",   "pop": 7_303_000, "lgas": 23, "dominant_religion": "Christian","major_ethnic": ["Ikwerre", "Ijaw", "Ogoni"]},
    "SOK": {"name": "Sokoto",               "zone": "North-West",    "pop": 4_963_000, "lgas": 23, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani"]},
    "TAR": {"name": "Taraba",               "zone": "North-East",    "pop": 3_066_000, "lgas": 16, "dominant_religion": "Mixed",    "major_ethnic": ["Jukun", "Fulani", "Chamba"]},
    "YOB": {"name": "Yobe",                 "zone": "North-East",    "pop": 3_294_000, "lgas": 17, "dominant_religion": "Muslim",   "major_ethnic": ["Kanuri", "Fulani", "Hausa"]},
    "ZAM": {"name": "Zamfara",              "zone": "North-West",    "pop": 4_151_000, "lgas": 14, "dominant_religion": "Muslim",   "major_ethnic": ["Hausa", "Fulani"]},
}

# ─── Universal Stakeholder Categories ────────────────────────────────────────
# Each entry: name, category, estimated_reach_pct, engagement_priority (1=highest),
# engagement_method, talking_points, cultural_protocol, office_relevance

UNIVERSAL_STAKEHOLDERS = [
    # ── Youth Groups ──────────────────────────────────────────────────────────
    {
        "id": "nys",
        "name": "National Youth Service Corps (NYSC) Alumni Association",
        "category": "Youth",
        "subcategory": "National Service",
        "reach_pct": 4.2,
        "priority": 1,
        "engagement_method": ["Town hall meetings", "Social media campaigns", "Skill acquisition partnerships"],
        "talking_points": [
            "Youth employment and entrepreneurship fund",
            "NYSC reform and skills-to-jobs pipeline",
            "Digital economy and tech hub investments",
            "Student loan accessibility and education financing",
        ],
        "cultural_protocol": "Address the State Coordinator first. Acknowledge service to the nation before policy discussion.",
        "office_relevance": {"President": 10, "Governor": 9, "Senator": 8, "House": 7, "LGA": 5},
        "best_engagement_time": "Weekends and passing-out parade periods",
        "key_ask": "Volunteer mobilisation and polling unit coverage on Election Day",
    },
    {
        "id": "nans",
        "name": "National Association of Nigerian Students (NANS)",
        "category": "Youth",
        "subcategory": "Student Body",
        "reach_pct": 6.8,
        "priority": 1,
        "engagement_method": ["Campus rallies", "Debate sponsorship", "Scholarship pledges"],
        "talking_points": [
            "University funding and ASUU strike resolution",
            "Student loan scheme expansion",
            "Campus security and hostel infrastructure",
            "Graduate employment guarantee programme",
        ],
        "cultural_protocol": "Engage the NANS Senate President and zone coordinators. Avoid making promises you cannot keep — students fact-check.",
        "office_relevance": {"President": 10, "Governor": 9, "Senator": 8, "House": 7, "LGA": 4},
        "best_engagement_time": "Semester periods (avoid exam weeks)",
        "key_ask": "Campus voter registration drives and first-time voter mobilisation",
    },
    {
        "id": "ypp",
        "name": "Youth Progressive Party Clubs & Non-Partisan Youth Networks",
        "category": "Youth",
        "subcategory": "Civic Engagement",
        "reach_pct": 3.5,
        "priority": 2,
        "engagement_method": ["Social media influencer partnerships", "Community project sponsorship", "Youth parliament sessions"],
        "talking_points": [
            "Not Too Young to Run Act implementation",
            "Youth quota in government appointments",
            "Digital skills and coding bootcamp funding",
            "Startup ecosystem and angel investment policy",
        ],
        "cultural_protocol": "Engage on social media first — Twitter/X Spaces, Instagram Lives. Physical meetings follow digital relationship.",
        "office_relevance": {"President": 9, "Governor": 8, "Senator": 7, "House": 8, "LGA": 6},
        "best_engagement_time": "Evenings and weekends",
        "key_ask": "Digital campaign amplification and peer-to-peer voter education",
    },
    {
        "id": "bmc",
        "name": "Bricklayers, Mechanics & Artisan Youth Cooperatives",
        "category": "Youth",
        "subcategory": "Vocational",
        "reach_pct": 5.1,
        "priority": 2,
        "engagement_method": ["Trade fair sponsorship", "Apprenticeship programme pledges", "Tool kit donations"],
        "talking_points": [
            "Vocational training centre construction",
            "Artisan credit scheme and micro-loans",
            "Market stall and workshop infrastructure",
            "Skills certification and NABTEB reform",
        ],
        "cultural_protocol": "Meet at their workplace or union hall. Bring a practical gift (branded tools, work gear). Speak in local language if possible.",
        "office_relevance": {"President": 6, "Governor": 8, "Senator": 7, "House": 8, "LGA": 10},
        "best_engagement_time": "Early morning (before 8am) or late afternoon (after 5pm)",
        "key_ask": "Ward-level mobilisation and community credibility endorsement",
    },
    {
        "id": "okada",
        "name": "Commercial Motorcyclists & Tricycle Operators Association (NURTW/RTEAN)",
        "category": "Youth",
        "subcategory": "Transport Workers",
        "reach_pct": 7.3,
        "priority": 1,
        "engagement_method": ["Park meetings", "Fuel subsidy pledges", "Accident insurance scheme"],
        "talking_points": [
            "Motorcycle and tricycle ban review",
            "Operator insurance and accident compensation",
            "Fuel subsidy and transport cost relief",
            "Park infrastructure and security",
        ],
        "cultural_protocol": "Meet the Park Chairman first — never bypass the hierarchy. Bring drinks and food to the park meeting.",
        "office_relevance": {"President": 7, "Governor": 9, "Senator": 7, "House": 8, "LGA": 10},
        "best_engagement_time": "Midday (slow business hours) or Sunday afternoons",
        "key_ask": "Election Day logistics — transporting voters to polling units",
    },

    # ── Women Associations ────────────────────────────────────────────────────
    {
        "id": "ncws",
        "name": "National Council of Women's Societies (NCWS)",
        "category": "Women",
        "subcategory": "National Body",
        "reach_pct": 8.4,
        "priority": 1,
        "engagement_method": ["Women's town halls", "Market outreach", "Radio programmes"],
        "talking_points": [
            "35% affirmative action in government appointments",
            "Gender-based violence legislation and enforcement",
            "Maternal healthcare and free antenatal care",
            "Women's economic empowerment and cooperative loans",
            "Girl-child education and school feeding programme",
        ],
        "cultural_protocol": "Address the State Chairperson by title. Acknowledge women's contributions before making requests. Bring female surrogates to the meeting.",
        "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 9, "LGA": 8},
        "best_engagement_time": "Tuesday and Thursday mornings (market days in most states)",
        "key_ask": "Women's bloc voter mobilisation — the largest single voting bloc in Nigeria",
    },
    {
        "id": "fomwan",
        "name": "Federation of Muslim Women's Associations in Nigeria (FOMWAN)",
        "category": "Women",
        "subcategory": "Religious Women",
        "reach_pct": 5.6,
        "priority": 1,
        "engagement_method": ["Mosque outreach", "Hijab rights advocacy", "Islamic school support"],
        "talking_points": [
            "Islamic education funding and Arabic school support",
            "Purdah-compatible voting arrangements",
            "Widow and orphan welfare programmes",
            "Halal food standards and market regulation",
        ],
        "cultural_protocol": "Engage through the husband or male guardian first in conservative states. Female candidate surrogates are essential. Observe prayer times strictly.",
        "office_relevance": {"President": 8, "Governor": 9, "Senator": 8, "House": 8, "LGA": 9},
        "best_engagement_time": "After Zuhr prayer (early afternoon)",
        "key_ask": "Mosque-based voter registration and female voter turnout in purdah communities",
    },
    {
        "id": "cwfn",
        "name": "Christian Women Fellowship of Nigeria (CWFN / CWO)",
        "category": "Women",
        "subcategory": "Religious Women",
        "reach_pct": 6.2,
        "priority": 1,
        "engagement_method": ["Church outreach", "Women's Sunday programme", "Welfare donations"],
        "talking_points": [
            "Church security and place of worship protection",
            "Widow and orphan welfare fund",
            "Maternal health and free delivery programme",
            "Women in leadership and church governance",
        ],
        "cultural_protocol": "Request to address the Women's Fellowship through the church pastor. Attend a Sunday service before making political requests.",
        "office_relevance": {"President": 8, "Governor": 9, "Senator": 8, "House": 8, "LGA": 9},
        "best_engagement_time": "Sunday after service, or Wednesday Bible study",
        "key_ask": "Church-based voter registration and women's bloc mobilisation",
    },
    {
        "id": "market_women",
        "name": "Market Women Associations (State & LGA Market Unions)",
        "category": "Women",
        "subcategory": "Traders",
        "reach_pct": 9.1,
        "priority": 1,
        "engagement_method": ["Market visits", "Trader loan pledges", "Market infrastructure promises"],
        "talking_points": [
            "Market reconstruction and stall allocation reform",
            "Trader micro-credit and cooperative loans",
            "Levy and tax reduction for small traders",
            "Security at markets and anti-extortion enforcement",
            "Cold chain and storage infrastructure",
        ],
        "cultural_protocol": "Visit the market physically — never invite them to your office first. Bring the Market Queen (Iyaloja/Madame) a gift. Walk through the market stalls.",
        "office_relevance": {"President": 7, "Governor": 10, "Senator": 8, "House": 9, "LGA": 10},
        "best_engagement_time": "Market days (varies by LGA — confirm locally)",
        "key_ask": "Market-level voter mobilisation — the most influential ground network in Nigeria",
    },

    # ── Traditional Rulers & Community Leaders ────────────────────────────────
    {
        "id": "trad_rulers",
        "name": "State Council of Traditional Rulers",
        "category": "Traditional Leaders",
        "subcategory": "Royal Council",
        "reach_pct": 12.0,
        "priority": 1,
        "engagement_method": ["Royal palace visits", "Chieftaincy title acceptance", "Community development donations"],
        "talking_points": [
            "Traditional institution recognition and funding",
            "Land rights and community ownership protection",
            "Rural infrastructure — roads, water, electricity",
            "Cultural heritage preservation and tourism",
        ],
        "cultural_protocol": "NEVER sit higher than the Oba/Emir/Igwe. Remove shoes where required. Prostrate or kneel as culturally appropriate. Bring kola nuts, palm wine, or appropriate gifts. Address by full title.",
        "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 10},
        "best_engagement_time": "Morning durbar hours or after Friday prayers for Muslim rulers",
        "key_ask": "Royal endorsement — the single most powerful voter influence mechanism in rural Nigeria",
    },
    {
        "id": "village_heads",
        "name": "Village Heads, Ward Heads & District Officers",
        "category": "Traditional Leaders",
        "subcategory": "Grassroots",
        "reach_pct": 15.0,
        "priority": 1,
        "engagement_method": ["Village square meetings", "Community project commissioning", "Direct household visits"],
        "talking_points": [
            "Borehole and clean water access",
            "Rural road grading and bridge repair",
            "Primary school renovation",
            "Community health centre staffing",
        ],
        "cultural_protocol": "Arrive with the LGA chairman or a known community member. Bring practical gifts (food items, building materials). Speak to the community in their language.",
        "office_relevance": {"President": 5, "Governor": 8, "Senator": 7, "House": 9, "LGA": 10},
        "best_engagement_time": "Evening community meetings (after farm work)",
        "key_ask": "Polling unit-level voter mobilisation and election day logistics",
    },

    # ── Religious Bodies ──────────────────────────────────────────────────────
    {
        "id": "jni",
        "name": "Jama'atu Nasril Islam (JNI) — Supreme Islamic Council",
        "category": "Religious",
        "subcategory": "Islamic",
        "reach_pct": 10.5,
        "priority": 1,
        "engagement_method": ["Friday mosque visits", "Ramadan welfare donations", "Islamic school support"],
        "talking_points": [
            "Almajiri school reform and integration",
            "Islamic banking and halal finance",
            "Pilgrimage (Hajj) subsidy and welfare",
            "Zakat fund and Islamic social welfare",
            "Northern security and banditry eradication",
        ],
        "cultural_protocol": "Engage through the Sultan of Sokoto's office for national campaigns. State-level through the Chief Imam. Never schedule meetings during prayer times. Dress modestly.",
        "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 9},
        "best_engagement_time": "After Jumu'ah (Friday prayer) or early morning",
        "key_ask": "Mosque-based voter education and Friday sermon endorsement",
    },
    {
        "id": "cbn_ng",
        "name": "Christian Association of Nigeria (CAN)",
        "category": "Religious",
        "subcategory": "Christian",
        "reach_pct": 10.5,
        "priority": 1,
        "engagement_method": ["Church visits", "Sunday school partnerships", "Christian welfare donations"],
        "talking_points": [
            "Religious freedom and church security",
            "Christian education and mission school funding",
            "Anti-kidnapping and farmer-herder conflict resolution",
            "Poverty alleviation and church welfare programmes",
        ],
        "cultural_protocol": "Request audience through the State CAN Chairman. Attend a church service. Never make political speeches during worship — request a separate engagement time.",
        "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 9},
        "best_engagement_time": "Sunday afternoons or midweek fellowship",
        "key_ask": "Church-based voter registration and Sunday announcement endorsements",
    },
    {
        "id": "pfn",
        "name": "Pentecostal Fellowship of Nigeria (PFN)",
        "category": "Religious",
        "subcategory": "Christian — Pentecostal",
        "reach_pct": 7.8,
        "priority": 2,
        "engagement_method": ["Crusade sponsorship", "Youth church programmes", "Prosperity gospel alignment"],
        "talking_points": [
            "Economic prosperity and job creation",
            "Anti-corruption and transparency",
            "Family values and social welfare",
            "Education and scholarship programmes",
        ],
        "cultural_protocol": "Engage through the General Overseer or Senior Pastor. Attend a programme before requesting endorsement. Tithing and welfare donations are expected.",
        "office_relevance": {"President": 9, "Governor": 9, "Senator": 8, "House": 8, "LGA": 7},
        "best_engagement_time": "Friday night vigil or Sunday service",
        "key_ask": "Congregation voter mobilisation and social media amplification",
    },

    # ── Civil Society & Professional Associations ─────────────────────────────
    {
        "id": "nba",
        "name": "Nigerian Bar Association (NBA)",
        "category": "Professional",
        "subcategory": "Legal",
        "reach_pct": 1.2,
        "priority": 2,
        "engagement_method": ["Bar dinner sponsorship", "Legal reform debate", "Pro-bono legal aid pledge"],
        "talking_points": [
            "Judicial independence and court funding",
            "Legal aid for indigent Nigerians",
            "Anti-corruption court reform",
            "Electoral law reform and INEC independence",
        ],
        "cultural_protocol": "Engage through the Branch Chairman. Lawyers respond to evidence and policy detail — bring a comprehensive manifesto, not slogans.",
        "office_relevance": {"President": 9, "Governor": 8, "Senator": 9, "House": 7, "LGA": 4},
        "best_engagement_time": "Bar association monthly meetings",
        "key_ask": "Legal credibility endorsement and election petition monitoring",
    },
    {
        "id": "nma",
        "name": "Nigerian Medical Association (NMA)",
        "category": "Professional",
        "subcategory": "Healthcare",
        "reach_pct": 0.8,
        "priority": 2,
        "engagement_method": ["Health summit sponsorship", "Hospital equipment donation", "Medical outreach"],
        "talking_points": [
            "Healthcare funding — 15% of budget (Abuja Declaration)",
            "Doctor brain drain reversal and salary reform",
            "Universal health coverage and NHIS expansion",
            "Primary healthcare centre rehabilitation",
        ],
        "cultural_protocol": "Engage through the State Chairman. Doctors are highly respected — treat them as partners, not supporters. Bring data and policy documents.",
        "office_relevance": {"President": 9, "Governor": 9, "Senator": 8, "House": 7, "LGA": 5},
        "best_engagement_time": "Medical association quarterly meetings",
        "key_ask": "Professional credibility endorsement and healthcare policy co-design",
    },
    {
        "id": "nans_teachers",
        "name": "Nigeria Union of Teachers (NUT)",
        "category": "Professional",
        "subcategory": "Education",
        "reach_pct": 3.4,
        "priority": 1,
        "engagement_method": ["School visits", "Teacher salary pledge", "Classroom infrastructure donation"],
        "talking_points": [
            "Teacher salary arrears payment and reform",
            "School infrastructure — desks, books, laboratories",
            "Teacher training and professional development",
            "Free school meals and uniform programme",
        ],
        "cultural_protocol": "Visit schools during school hours. Address the State NUT Chairman. Teachers are opinion leaders in their communities — their endorsement extends beyond the classroom.",
        "office_relevance": {"President": 8, "Governor": 9, "Senator": 8, "House": 8, "LGA": 9},
        "best_engagement_time": "School term periods, teachers' day (October 5)",
        "key_ask": "Community-level voter education and school-based voter registration",
    },
    {
        "id": "nupeng",
        "name": "Nigeria Union of Petroleum & Natural Gas Workers (NUPENG)",
        "category": "Labour",
        "subcategory": "Energy Workers",
        "reach_pct": 2.1,
        "priority": 2,
        "engagement_method": ["Union hall meetings", "Fuel subsidy policy dialogue", "Worker welfare pledges"],
        "talking_points": [
            "Fuel subsidy reform and worker compensation",
            "Refinery rehabilitation and local refining",
            "Oil community development and host community fund",
            "Worker safety and occupational health",
        ],
        "cultural_protocol": "Engage through the National President or State Chairman. Labour unions respond to concrete policy commitments, not vague promises.",
        "office_relevance": {"President": 10, "Governor": 7, "Senator": 8, "House": 6, "LGA": 4},
        "best_engagement_time": "Union congress meetings",
        "key_ask": "Labour bloc endorsement and industrial action neutrality during campaign",
    },
    {
        "id": "nlc",
        "name": "Nigeria Labour Congress (NLC) — State Councils",
        "category": "Labour",
        "subcategory": "General Labour",
        "reach_pct": 5.5,
        "priority": 1,
        "engagement_method": ["May Day rally participation", "Worker welfare pledges", "Minimum wage commitment"],
        "talking_points": [
            "Minimum wage increase and implementation",
            "Workers' rights and anti-casualisation",
            "Pension reform and gratuity payment",
            "Occupational health and safety legislation",
        ],
        "cultural_protocol": "Attend May Day rallies. Engage the State Chairman. Labour leaders are politically sophisticated — bring detailed policy positions.",
        "office_relevance": {"President": 10, "Governor": 9, "Senator": 8, "House": 7, "LGA": 6},
        "best_engagement_time": "May Day (May 1) and union congress periods",
        "key_ask": "Organised labour endorsement — covers millions of formal sector workers",
    },
    {
        "id": "cso",
        "name": "Civil Society Organisations & NGO Networks",
        "category": "Civil Society",
        "subcategory": "Advocacy",
        "reach_pct": 2.8,
        "priority": 2,
        "engagement_method": ["Policy dialogue forums", "Manifesto review sessions", "Transparency pledge signing"],
        "talking_points": [
            "Freedom of information and open government",
            "Anti-corruption and asset declaration",
            "Electoral reform and INEC independence",
            "Human rights and rule of law",
        ],
        "cultural_protocol": "CSOs are watchdogs — engage transparently. Sign their candidate pledge forms. Avoid making promises you cannot keep — they will hold you accountable publicly.",
        "office_relevance": {"President": 9, "Governor": 8, "Senator": 8, "House": 7, "LGA": 5},
        "best_engagement_time": "Pre-election policy forums",
        "key_ask": "Election monitoring neutrality and policy credibility endorsement",
    },
    {
        "id": "farmers",
        "name": "All Farmers Association of Nigeria (AFAN) — State Chapters",
        "category": "Agriculture",
        "subcategory": "Farmers",
        "reach_pct": 11.2,
        "priority": 1,
        "engagement_method": ["Farm visits", "Fertiliser donation", "Cooperative loan pledges"],
        "talking_points": [
            "Fertiliser subsidy and distribution reform",
            "Farmer-herder conflict resolution and security",
            "Irrigation infrastructure and dam rehabilitation",
            "Agricultural credit and cooperative loans",
            "Food storage and commodity board revival",
        ],
        "cultural_protocol": "Visit farms and rural communities. Dress simply. Bring practical gifts (seeds, fertiliser, farm tools). Speak about food security in concrete terms.",
        "office_relevance": {"President": 9, "Governor": 10, "Senator": 8, "House": 9, "LGA": 10},
        "best_engagement_time": "Post-harvest season (October–December) or pre-planting (February–March)",
        "key_ask": "Rural voter mobilisation — farmers represent the largest single occupational group of voters",
    },
    {
        "id": "diaspora",
        "name": "Nigerian Diaspora Organisations (NDO / NIDCOM Networks)",
        "category": "Diaspora",
        "subcategory": "Overseas Nigerians",
        "reach_pct": 1.5,
        "priority": 2,
        "engagement_method": ["Virtual town halls", "Diaspora remittance policy dialogue", "Investment summit"],
        "talking_points": [
            "Diaspora voting rights and overseas voting",
            "Dual citizenship and property rights",
            "Remittance cost reduction and fintech policy",
            "Investment incentives and diaspora bonds",
        ],
        "cultural_protocol": "Engage via Zoom/Teams first. Diaspora Nigerians are highly educated and politically engaged — bring detailed policy documents. They fund campaigns significantly.",
        "office_relevance": {"President": 10, "Governor": 7, "Senator": 7, "House": 5, "LGA": 3},
        "best_engagement_time": "Weekends (accounting for time zone differences)",
        "key_ask": "Campaign fundraising and social media amplification from overseas",
    },
    {
        "id": "traders",
        "name": "Amalgamated Union of Traders & Manufacturers (State Chapters)",
        "category": "Commerce",
        "subcategory": "Traders",
        "reach_pct": 6.7,
        "priority": 1,
        "engagement_method": ["Trade fair visits", "Tax relief pledges", "Import duty reform dialogue"],
        "talking_points": [
            "Multiple taxation and levy reduction",
            "Import duty reform and border trade",
            "Market infrastructure and storage",
            "SME credit and business registration simplification",
        ],
        "cultural_protocol": "Visit the main market. Meet the Iyaloja (women traders) and Babaloja (men traders) separately. Bring a practical gift. Walk through the market stalls.",
        "office_relevance": {"President": 8, "Governor": 9, "Senator": 7, "House": 8, "LGA": 10},
        "best_engagement_time": "Market days and trade fair periods",
        "key_ask": "Market-level voter mobilisation and commercial community endorsement",
    },
    {
        "id": "persons_disability",
        "name": "Joint National Association of Persons with Disabilities (JONAPWD)",
        "category": "Inclusion",
        "subcategory": "Disability Rights",
        "reach_pct": 2.3,
        "priority": 2,
        "engagement_method": ["Accessibility audit pledge", "Assistive device donation", "Inclusive policy commitment"],
        "talking_points": [
            "Disability Rights Act implementation",
            "Accessible polling unit infrastructure",
            "Assistive technology and healthcare",
            "Employment quota for persons with disabilities",
        ],
        "cultural_protocol": "Ensure your campaign venue is wheelchair accessible. Use sign language interpreters. Engage the State Chairman directly.",
        "office_relevance": {"President": 8, "Governor": 8, "Senator": 7, "House": 7, "LGA": 6},
        "best_engagement_time": "International Day of Persons with Disabilities (December 3)",
        "key_ask": "Inclusive campaign endorsement and accessibility compliance signalling",
    },
]

# ─── Zone-Specific Stakeholders ───────────────────────────────────────────────
ZONE_STAKEHOLDERS: Dict[str, List[Dict]] = {
    "North-West": [
        {
            "id": "arewa_nw",
            "name": "Arewa Consultative Forum (ACF) — State Chapters",
            "category": "Ethnic/Regional",
            "subcategory": "Northern Socio-Political",
            "reach_pct": 14.0,
            "priority": 1,
            "engagement_method": ["Durbar attendance", "Northern development dialogue", "Emirate palace visits"],
            "talking_points": ["Northern unity and development", "Security and banditry eradication", "Almajiri reform", "Agricultural investment"],
            "cultural_protocol": "Engage through the Emir's palace. Dress in traditional Northern attire (babariga/kaftan). Speak Hausa if possible. Bring kola nuts.",
            "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 9},
            "best_engagement_time": "After Friday Jumu'ah prayer",
            "key_ask": "Northern bloc endorsement — critical for any national office",
        },
        {
            "id": "miyetti_nw",
            "name": "Miyetti Allah Cattle Breeders Association (MACBAN)",
            "category": "Pastoral",
            "subcategory": "Herders",
            "reach_pct": 5.5,
            "priority": 2,
            "engagement_method": ["Grazing reserve dialogue", "Ranching policy commitment", "Conflict resolution pledge"],
            "talking_points": ["Grazing reserve establishment", "Ranching and cattle colony policy", "Farmer-herder conflict resolution", "Veterinary services"],
            "cultural_protocol": "Engage through the National President. Avoid inflammatory language about open grazing. Focus on economic solutions.",
            "office_relevance": {"President": 9, "Governor": 8, "Senator": 7, "House": 6, "LGA": 7},
            "best_engagement_time": "Dry season (November–March) when herders are more accessible",
            "key_ask": "Pastoral community neutrality and rural North-West voter access",
        },
    ],
    "North-East": [
        {
            "id": "borno_elders",
            "name": "Borno Elders Forum & Lake Chad Basin Communities",
            "category": "Traditional Leaders",
            "subcategory": "Conflict-Affected Communities",
            "reach_pct": 8.0,
            "priority": 1,
            "engagement_method": ["IDP camp visits", "Reconstruction pledge", "Security dialogue"],
            "talking_points": ["Boko Haram/ISWAP eradication", "IDP return and resettlement", "Reconstruction of destroyed communities", "Lake Chad restoration"],
            "cultural_protocol": "Visit IDP camps personally. Show empathy before policy. Engage the Shehu of Borno's palace. Security briefings are expected.",
            "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 9},
            "best_engagement_time": "Dry season (safer for travel)",
            "key_ask": "Conflict-affected community mobilisation and security credibility",
        },
    ],
    "North-Central": [
        {
            "id": "middlebelt",
            "name": "Middle Belt Forum (MBF)",
            "category": "Ethnic/Regional",
            "subcategory": "Middle Belt Socio-Political",
            "reach_pct": 11.0,
            "priority": 1,
            "engagement_method": ["Middle Belt summit", "Farmer-herder resolution pledge", "Minority rights dialogue"],
            "talking_points": ["Middle Belt security and farmer protection", "Minority rights and federal character", "Land rights and anti-grazing law", "Rural development"],
            "cultural_protocol": "Engage through the Forum President. Acknowledge the historical marginalisation of the Middle Belt. Avoid taking sides on farmer-herder conflict publicly.",
            "office_relevance": {"President": 10, "Governor": 9, "Senator": 9, "House": 8, "LGA": 8},
            "best_engagement_time": "Post-harvest season when security is relatively stable",
            "key_ask": "Middle Belt bloc endorsement — a swing region in national elections",
        },
    ],
    "South-West": [
        {
            "id": "afenifere",
            "name": "Afenifere (Yoruba Socio-Cultural Organisation)",
            "category": "Ethnic/Regional",
            "subcategory": "Yoruba Socio-Political",
            "reach_pct": 13.0,
            "priority": 1,
            "engagement_method": ["Yoruba summit attendance", "Restructuring policy dialogue", "Obafemi Awolowo legacy alignment"],
            "talking_points": ["Restructuring and true federalism", "Yoruba self-determination and security", "Amotekun (regional security) support", "Education and free education legacy"],
            "cultural_protocol": "Engage through the Patron (Baba Adebanjo era leadership). Acknowledge the Awolowo legacy. Dress in aso-oke for formal meetings. Speak Yoruba.",
            "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 7},
            "best_engagement_time": "Yoruba Day (July 20) and cultural festivals",
            "key_ask": "South-West bloc endorsement — critical for any national office",
        },
        {
            "id": "oodua",
            "name": "Oodua Peoples Congress (OPC) & Yoruba Self-Determination Groups",
            "category": "Ethnic/Regional",
            "subcategory": "Yoruba Activist",
            "reach_pct": 4.5,
            "priority": 2,
            "engagement_method": ["Security partnership dialogue", "Yoruba cultural events", "Self-determination policy position"],
            "talking_points": ["Yoruba security and Amotekun", "Restructuring and devolution", "Anti-open grazing enforcement", "Yoruba cultural preservation"],
            "cultural_protocol": "Engage carefully — OPC is politically sensitive. Focus on security and self-determination. Avoid being seen as anti-North.",
            "office_relevance": {"President": 8, "Governor": 9, "Senator": 7, "House": 7, "LGA": 6},
            "best_engagement_time": "Cultural festivals and Yoruba Day",
            "key_ask": "Youth activist mobilisation in South-West urban areas",
        },
    ],
    "South-East": [
        {
            "id": "ohanaeze",
            "name": "Ohanaeze Ndigbo (Igbo Socio-Cultural Organisation)",
            "category": "Ethnic/Regional",
            "subcategory": "Igbo Socio-Political",
            "reach_pct": 13.5,
            "priority": 1,
            "engagement_method": ["Igbo summit attendance", "Restructuring dialogue", "Igbo presidency advocacy"],
            "talking_points": ["Igbo presidency and power rotation", "IPOB/ESN security dialogue", "South-East infrastructure (Second Niger Bridge)", "Igbo business and trade protection"],
            "cultural_protocol": "Engage through the President-General. Acknowledge the civil war and marginalisation. Speak Igbo phrases. Attend Igbo cultural events.",
            "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 7},
            "best_engagement_time": "New Yam Festival (August) and Igbo Day",
            "key_ask": "South-East bloc endorsement — critical for any national office",
        },
        {
            "id": "igbo_traders",
            "name": "Igbo Traders Association & Ohaneze Business Forum",
            "category": "Commerce",
            "subcategory": "Igbo Traders",
            "reach_pct": 7.2,
            "priority": 1,
            "engagement_method": ["Onitsha Main Market visits", "Trade protection pledge", "Igbo business summit"],
            "talking_points": ["Igbo business protection in Northern states", "Import duty reform and Nnewi auto cluster", "Aba manufacturing revival", "Trade route security"],
            "cultural_protocol": "Visit Onitsha Main Market and Aba markets. Meet the Eze Ndigbo (Igbo king) in each state. Bring practical business policy commitments.",
            "office_relevance": {"President": 9, "Governor": 8, "Senator": 7, "House": 8, "LGA": 9},
            "best_engagement_time": "Market days and trade fair periods",
            "key_ask": "Igbo business community endorsement and diaspora fundraising",
        },
    ],
    "South-South": [
        {
            "id": "ijaw_council",
            "name": "Ijaw National Congress (INC)",
            "category": "Ethnic/Regional",
            "subcategory": "Niger Delta Socio-Political",
            "reach_pct": 8.5,
            "priority": 1,
            "engagement_method": ["Niger Delta summit", "Host community fund dialogue", "Creeks security partnership"],
            "talking_points": ["Niger Delta development and NDDC reform", "Host community fund and oil revenue sharing", "Creeks security and ex-militant reintegration", "Environmental remediation (Ogoniland)"],
            "cultural_protocol": "Engage through the INC President. Acknowledge the environmental damage to the Niger Delta. Visit communities, not just offices. Show respect for Ijaw culture.",
            "office_relevance": {"President": 10, "Governor": 10, "Senator": 9, "House": 8, "LGA": 8},
            "best_engagement_time": "Dry season (easier creek navigation)",
            "key_ask": "Niger Delta bloc endorsement and creeks security neutrality",
        },
        {
            "id": "pan_niger",
            "name": "Pan Niger Delta Forum (PANDEF)",
            "category": "Ethnic/Regional",
            "subcategory": "Niger Delta Advocacy",
            "reach_pct": 9.0,
            "priority": 1,
            "engagement_method": ["Niger Delta summit", "Resource control dialogue", "Environmental justice pledge"],
            "talking_points": ["Resource control and fiscal federalism", "NDDC reform and accountability", "Environmental remediation", "Niger Delta security and ex-militant welfare"],
            "cultural_protocol": "Engage through the PANDEF leadership. The Niger Delta is politically sophisticated — bring detailed policy positions on resource control.",
            "office_relevance": {"President": 10, "Governor": 9, "Senator": 9, "House": 7, "LGA": 7},
            "best_engagement_time": "Pre-election policy forums",
            "key_ask": "South-South bloc endorsement — critical for any national office",
        },
    ],
}

# ─── Office-Level Engagement Sequence ────────────────────────────────────────
OFFICE_SEQUENCE: Dict[str, List[str]] = {
    "President": [
        "1. Traditional Rulers & Emirate/Palace (legitimacy)",
        "2. Religious Bodies — JNI & CAN (moral authority)",
        "3. Regional Socio-Political Bodies — Afenifere/Ohanaeze/ACF/INC (ethnic bloc)",
        "4. Labour Congress — NLC (organised labour bloc)",
        "5. Market Women & Traders (grassroots economic bloc)",
        "6. NANS & Youth Networks (first-time voter mobilisation)",
        "7. Farmers Association — AFAN (rural majority)",
        "8. Professional Bodies — NBA/NMA (credibility endorsement)",
        "9. Diaspora Networks (fundraising & social media)",
        "10. Civil Society — CSOs (transparency signalling)",
    ],
    "Governor": [
        "1. State Council of Traditional Rulers (state legitimacy)",
        "2. State Religious Bodies — CAN/JNI chapters (moral authority)",
        "3. Market Women & Iyaloja (economic grassroots)",
        "4. State NLC & NUT (organised labour)",
        "5. NANS State Zone & Youth Networks (youth bloc)",
        "6. Farmers Association — State AFAN (rural majority)",
        "7. State Professional Bodies — NBA/NMA branches (credibility)",
        "8. Village Heads & Ward Heads (LGA-level mobilisation)",
        "9. Artisan & Vocational Youth Cooperatives (urban youth)",
        "10. Motorcyclists & Transport Workers (Election Day logistics)",
    ],
    "Senator": [
        "1. District & Emirate/Kingdom Traditional Rulers",
        "2. Senatorial Zone Religious Leaders",
        "3. Market Women & Traders in the Zone",
        "4. NUT & Teachers in the Zone",
        "5. Youth Networks & NANS Campus Chapters",
        "6. Farmers & Agricultural Cooperatives",
        "7. Transport Workers (NURTW/RTEAN)",
        "8. Civil Society & Electoral Monitoring Groups",
    ],
    "House": [
        "1. Ward & Village Heads in the Constituency",
        "2. Local Religious Leaders (Imam/Pastor)",
        "3. Market Women & Local Traders",
        "4. Youth Groups & Artisan Cooperatives",
        "5. NUT Teachers (community opinion leaders)",
        "6. Transport Workers (Election Day logistics)",
        "7. Farmers & Agricultural Cooperatives",
    ],
    "LGA": [
        "1. Village Heads & Ward Heads (direct community access)",
        "2. Market Women & Iyaloja (economic influence)",
        "3. Local Imam/Pastor (religious community)",
        "4. Transport Workers — Okada/Keke (Election Day logistics)",
        "5. Artisan & Vocational Youth Cooperatives",
        "6. Farmers & Agricultural Cooperatives",
        "7. NUT Teachers (community credibility)",
    ],
}

# ─── Main Recommendation Function ────────────────────────────────────────────

def recommend_stakeholders(
    state_code: str,
    office_type: str,
    candidate_name: str,
    party_code: str,
    religion: Optional[str] = None,
    ethnicity: Optional[str] = None,
    gender: Optional[str] = None,
    top_n: int = 15,
) -> Dict:
    """
    Return a prioritised stakeholder engagement plan for a candidate.

    Parameters
    ----------
    state_code   : 3-letter state code (e.g. "LAG", "KAN", "FCT")
    office_type  : "President" | "Governor" | "Senator" | "House" | "LGA"
    candidate_name: Candidate's full name
    party_code   : Party abbreviation (e.g. "APC", "PDP", "LP")
    religion     : Optional — "Muslim" | "Christian" | "Traditional"
    ethnicity    : Optional — candidate's ethnic group
    gender       : Optional — "Male" | "Female"
    top_n        : Number of top stakeholders to return (default 15)
    """
    state = STATE_META.get(state_code.upper())
    if not state:
        return {"error": f"Unknown state code: {state_code}"}

    zone = state["zone"]
    office_type = office_type.title()

    # 1. Collect all stakeholders (universal + zone-specific)
    all_stakeholders = list(UNIVERSAL_STAKEHOLDERS)
    zone_specific = ZONE_STAKEHOLDERS.get(zone, [])
    all_stakeholders = zone_specific + all_stakeholders  # zone-specific get priority

    # 2. Score each stakeholder for this candidate
    scored = []
    for s in all_stakeholders:
        base_score = s["office_relevance"].get(office_type, 5)
        reach_score = math.log1p(s["reach_pct"]) * 10

        # Bonus: religion alignment
        religion_bonus = 0
        if religion and s.get("subcategory"):
            if religion == "Muslim" and "Islamic" in s["subcategory"]:
                religion_bonus = 15
            elif religion == "Christian" and "Christian" in s["subcategory"]:
                religion_bonus = 15

        # Bonus: gender alignment
        gender_bonus = 0
        if gender == "Female" and s["category"] == "Women":
            gender_bonus = 20

        # Priority weight
        priority_weight = 10 if s["priority"] == 1 else 0

        total_score = base_score * 10 + reach_score + religion_bonus + gender_bonus + priority_weight

        scored.append({
            **s,
            "relevance_score": round(total_score, 1),
            "estimated_voter_reach": round(state["pop"] * s["reach_pct"] / 100),
            "state_name": state["name"],
            "zone": zone,
        })

    # 3. Sort by score and return top N
    scored.sort(key=lambda x: x["relevance_score"], reverse=True)
    top = scored[:top_n]

    # 4. Build engagement sequence
    sequence = OFFICE_SEQUENCE.get(office_type, OFFICE_SEQUENCE["House"])

    # 5. Cultural context
    cultural_context = {
        "dominant_religion": state["dominant_religion"],
        "major_ethnic_groups": state["major_ethnic"],
        "zone": zone,
        "zone_specific_notes": _zone_notes(zone, state_code, religion, gender),
        "total_population": state["pop"],
        "lga_count": state["lgas"],
    }

    # 6. Quick-win vs long-term split
    quick_wins = [s for s in top if s["priority"] == 1][:5]
    long_term = [s for s in top if s["priority"] == 2][:5]

    return {
        "candidate": candidate_name,
        "party": party_code,
        "state": state["name"],
        "state_code": state_code.upper(),
        "office": office_type,
        "zone": zone,
        "total_stakeholders_identified": len(top),
        "estimated_total_reach": sum(s["estimated_voter_reach"] for s in top),
        "engagement_sequence": sequence,
        "cultural_context": cultural_context,
        "top_stakeholders": top,
        "quick_wins": quick_wins,
        "long_term_relationships": long_term,
        "summary": _generate_summary(candidate_name, office_type, state["name"], zone, top, gender),
    }


def _zone_notes(zone: str, state_code: str, religion: Optional[str], gender: Optional[str]) -> List[str]:
    notes = []
    if zone in ("North-West", "North-East"):
        notes.append("Friday Jumu'ah prayer is the most important weekly engagement window — schedule major meetings after it.")
        notes.append("Emirate palace protocol is non-negotiable — never bypass the Emir to reach the community.")
        if gender == "Female":
            notes.append("Female candidate: deploy male surrogates for Emirate palace visits; lead women's town halls personally.")
    if zone == "North-Central":
        notes.append("The Middle Belt is a swing zone — balance outreach between Christian and Muslim communities carefully.")
        notes.append("Farmer-herder conflict is the defining issue — take a clear, enforceable position.")
    if zone == "South-West":
        notes.append("Restructuring and true federalism are non-negotiable talking points for Yoruba audiences.")
        notes.append("Afenifere endorsement is the gold standard — pursue it before any other South-West engagement.")
    if zone == "South-East":
        notes.append("Acknowledge the civil war and Igbo marginalisation before any policy discussion.")
        notes.append("Monday sit-at-home compliance is a political reality — avoid scheduling events on Mondays.")
    if zone == "South-South":
        notes.append("Resource control and NDDC reform are the defining issues — bring detailed policy positions.")
        notes.append("Community visits to creeks and riverine areas are essential — do not only engage in Port Harcourt/Warri.")
    if state_code == "LAG":
        notes.append("Lagos is hyper-urban — social media and radio outreach are as important as physical engagement.")
        notes.append("The NURTW (transport workers) controls street-level mobilisation in Lagos — engage them early.")
    if state_code == "KAN":
        notes.append("Kano has 44 LGAs — the largest in Nigeria. Emirate endorsement is essential for rural coverage.")
        notes.append("Kano's commercial class (Kanawa traders) are a powerful independent political force.")
    return notes


def _generate_summary(name: str, office: str, state: str, zone: str, stakeholders: List[Dict], gender: Optional[str]) -> str:
    top3 = [s["name"] for s in stakeholders[:3]]
    reach = sum(s["estimated_voter_reach"] for s in stakeholders)
    gender_note = " As a female candidate, prioritise women's associations and deploy male surrogates for culturally sensitive engagements." if gender == "Female" else ""
    return (
        f"{name}'s stakeholder engagement plan for {office} in {state} ({zone}) identifies "
        f"{len(stakeholders)} key organisations with a combined estimated voter reach of "
        f"{reach:,}. The three highest-priority groups are: {', '.join(top3)}. "
        f"Begin with traditional rulers and religious bodies to establish legitimacy, then "
        f"cascade outward to market women, labour unions, and youth networks.{gender_note}"
    )
