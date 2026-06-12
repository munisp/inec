-- ============================================================================
-- GOTV Large-Scale Realistic Seed Data
-- ============================================================================
-- Populates all GOTV tables with realistic Nigerian election mobilization data.
-- Designed for load testing, demo, and development environments.
--
-- Scale: 5 parties, 5000+ contacts, 500+ volunteers, 50+ campaigns,
--        2000+ pledges, 800+ ride requests, 10000+ outreach logs,
--        5000+ door knocks, 200+ shifts, territories, field reports, etc.
--
-- Usage: psql -U ngapp -d ngapp -f scripts/seed_gotv.sql
-- ============================================================================

BEGIN;

-- ─── Ensure base parties exist ─────────────────────────────────────────────

INSERT INTO parties (id, code, name, abbreviation, color, is_active) VALUES
  (1, 'APC', 'All Progressives Congress', 'APC', '#009639', 1),
  (2, 'PDP', 'Peoples Democratic Party', 'PDP', '#E30A0A', 1),
  (3, 'LP', 'Labour Party', 'LP', '#4C8C2B', 1),
  (4, 'NNPP', 'New Nigeria Peoples Party', 'NNPP', '#1E3A5F', 1),
  (5, 'ADC', 'African Democratic Congress', 'ADC', '#FF6600', 1)
ON CONFLICT (id) DO NOTHING;

-- Reset sequences
SELECT setval('parties_id_seq', (SELECT COALESCE(MAX(id),0) FROM parties));

-- ─── Ensure an election exists ─────────────────────────────────────────────

INSERT INTO elections (id, title, election_type, election_date, status, description, total_registered_voters) VALUES
  (1, '2027 General Election', 'presidential', '2027-02-25', 'upcoming', 'Nigerian Presidential and National Assembly Elections', 93469008)
ON CONFLICT (id) DO NOTHING;

SELECT setval('elections_id_seq', (SELECT COALESCE(MAX(id),0) FROM elections));

-- ─── Party Access Keys ─────────────────────────────────────────────────────

INSERT INTO gotv_party_access (party_id, api_key_hash, created_by, is_active, rate_limit_per_hour) VALUES
  (1, 'sha256_apc_demo_key_hash_001', 'admin@apc.ng', TRUE, 5000),
  (2, 'sha256_pdp_demo_key_hash_002', 'admin@pdp.ng', TRUE, 5000),
  (3, 'sha256_lp_demo_key_hash_003', 'admin@lp.ng', TRUE, 3000),
  (4, 'sha256_nnpp_demo_key_hash_004', 'admin@nnpp.ng', TRUE, 2000),
  (5, 'sha256_adc_demo_key_hash_005', 'admin@adc.ng', TRUE, 2000)
ON CONFLICT (party_id) DO NOTHING;

-- ─── Update campaign_type constraint to include all channel types ───────────

ALTER TABLE gotv_campaigns DROP CONSTRAINT IF EXISTS gotv_campaigns_campaign_type_check;
ALTER TABLE gotv_campaigns ADD CONSTRAINT gotv_campaigns_campaign_type_check
  CHECK(campaign_type IN ('sms','ussd','push','whatsapp','whatsapp_interactive','email','door_to_door','phone_bank','ride_to_polls','twitter','facebook','instagram','tiktok'));

-- ─── Nigerian Geographic Reference Data ────────────────────────────────────
-- State codes, LGAs, and sample polling unit codes for realistic distribution.

-- Helper: generate UUIDs for records
-- We use generate_series + md5 for deterministic pseudo-UUIDs

-- ─── CONTACTS (5000+) ──────────────────────────────────────────────────────
-- Realistic Nigerian names, phone numbers, geographic distribution across 37 states

-- Common Nigerian first names (150 names across major ethnic groups)
CREATE TEMP TABLE _first_names (name TEXT);
INSERT INTO _first_names VALUES
  ('Adebayo'),('Chidinma'),('Emeka'),('Funmilayo'),('Gbenga'),('Hassan'),('Ifeoma'),('Jide'),
  ('Kelechi'),('Ladi'),('Mohammed'),('Ngozi'),('Obiora'),('Patience'),('Quadri'),('Rashidat'),
  ('Segun'),('Titilayo'),('Uche'),('Victoria'),('Wale'),('Xavi'),('Yetunde'),('Zainab'),
  ('Abubakar'),('Blessing'),('Chukwuemeka'),('Deborah'),('Ekene'),('Fatima'),('Godwin'),('Habiba'),
  ('Ibrahim'),('Jumoke'),('Kabiru'),('Lilian'),('Musa'),('Nkechi'),('Olumide'),('Perpetua'),
  ('Rilwan'),('Stella'),('Tunde'),('Uchenna'),('Vincent'),('Wasiu'),('Yusuf'),('Zara'),
  ('Adaeze'),('Bola'),('Chijioke'),('Daniel'),('Esther'),('Folake'),('Grace'),('Hauwa'),
  ('Ikechukwu'),('Janet'),('Kayode'),('Loveth'),('Mustapha'),('Nneka'),('Olusegun'),('Priscilla'),
  ('Rasheed'),('Sunday'),('Toyin'),('Uzoma'),('Veronica'),('Williams'),('Yakubu'),('Zikora'),
  ('Adekunle'),('Bridget'),('Chinedu'),('Doris'),('Emmanuel'),('Folasade'),('Gloria'),('Halima'),
  ('Innocent'),('Joy'),('Kenneth'),('Lucy'),('Maryam'),('Nnamdi'),('Oluwaseun'),('Patricia'),
  ('Reuben'),('Shade'),('Taiwo'),('Ugochukwu'),('Vivian'),('Wilfred'),('Yemi'),('Zubairu'),
  ('Aisha'),('Bukola'),('Clement'),('Damilola'),('Elizabeth'),('Femi'),('Gift'),('Haruna'),
  ('Isioma'),('Joseph'),('Kehinde'),('Mercy'),('Nasiru'),('Ogechi'),('Philip'),('Rosemary'),
  ('Samuel'),('Temitope'),('Usman'),('Veronica'),('Wumi'),('Yinka'),('Anthonia'),('Binta'),
  ('Chibuzor'),('David'),('Eunice'),('Florence'),('George'),('Helen'),('Ifeanyi'),('James'),
  ('Kemi'),('Lydia'),('Michael'),('Nonso'),('Oluwadamilola'),('Peace'),('Rita'),('Solomon'),
  ('Tolu'),('Udo'),('Victor'),('Wakili'),('Yeside'),('Amina'),('Bamidele'),('Catherine');

-- Common Nigerian surnames (100 surnames)
CREATE TEMP TABLE _last_names (name TEXT);
INSERT INTO _last_names VALUES
  ('Okafor'),('Ibrahim'),('Adeyemi'),('Bello'),('Chukwu'),('Danjuma'),('Eze'),('Fasola'),
  ('Garba'),('Hassan'),('Igwe'),('Johnson'),('Kalu'),('Lawal'),('Mohammed'),('Nwachukwu'),
  ('Obi'),('Peters'),('Quadri'),('Raji'),('Suleiman'),('Thompson'),('Umar'),('Victor'),
  ('Williams'),('Yakubu'),('Abdullahi'),('Bakare'),('Collins'),('Dauda'),('Emeka'),('Fashola'),
  ('Gbadamosi'),('Haruna'),('Ike'),('Julius'),('Kazeem'),('Lateef'),('Muritala'),('Nwosu'),
  ('Ojo'),('Popoola'),('Rabiu'),('Salisu'),('Tijani'),('Umeh'),('Vandi'),('Waziri'),
  ('Yusuf'),('Aliyu'),('Balogun'),('Chima'),('Dikko'),('Ezenwa'),('Fagbenro'),('Gana'),
  ('Idris'),('Jimoh'),('Kosoko'),('Lawan'),('Madu'),('Nnamdi'),('Ogundimu'),('Pam'),
  ('Rafiu'),('Sanni'),('Tanko'),('Udoh'),('Vandi'),('Wada'),('Adesina'),('Babangida'),
  ('Chibueze'),('Dada'),('Enyinnaya'),('Fowler'),('Gambari'),('Hussaini'),('Isichei'),('Jibrin'),
  ('Kanu'),('Lamido'),('Maina'),('Nnadi'),('Olumide'),('Pius'),('Rimi'),('Shagari'),
  ('Turaki'),('Ugwu'),('Vambe'),('Wasagu'),('Yusufu'),('Adamu'),('Buhari'),('Chidozie'),
  ('Dangote'),('Elumelu'),('Fayose'),('Gowon');

-- Nigerian states with coordinates for center points
CREATE TEMP TABLE _states (code TEXT, lat REAL, lng REAL, weight INT);
INSERT INTO _states VALUES
  ('LA', 6.5244, 3.3792, 18),   -- Lagos (highest population)
  ('KN', 12.0022, 8.5920, 14),  -- Kano
  ('RI', 4.8156, 7.0498, 10),   -- Rivers
  ('KD', 10.5105, 7.4165, 10),  -- Kaduna
  ('OY', 7.3775, 3.9470, 9),    -- Oyo
  ('AN', 6.2209, 6.9370, 8),    -- Anambra
  ('DE', 5.8987, 5.6800, 7),    -- Delta
  ('ED', 6.3350, 5.6270, 7),    -- Edo
  ('FC', 9.0579, 7.4951, 7),    -- FCT Abuja
  ('EN', 6.4584, 7.5464, 6),    -- Enugu
  ('OG', 6.9980, 3.4737, 6),    -- Ogun
  ('OS', 7.5629, 4.5200, 5),    -- Osun
  ('KW', 8.4966, 4.5427, 5),    -- Kwara
  ('BO', 11.8469, 13.1510, 5),  -- Borno
  ('AB', 5.4527, 7.5248, 5),    -- Abia
  ('IM', 5.4920, 7.0264, 5),    -- Imo
  ('ON', 7.2500, 5.2000, 4),    -- Ondo
  ('EK', 7.6211, 5.2211, 4),    -- Ekiti
  ('PL', 9.2182, 9.5177, 4),    -- Plateau
  ('BE', 7.3369, 8.7400, 4),    -- Benue
  ('CR', 4.9517, 8.3220, 4),    -- Cross River
  ('AK', 5.0078, 7.8500, 4),    -- Akwa Ibom
  ('BA', 10.3158, 9.8442, 3),   -- Bauchi
  ('NI', 9.6000, 6.5500, 3),    -- Niger
  ('SO', 13.0607, 5.2476, 3),   -- Sokoto
  ('KT', 12.9857, 7.6018, 3),   -- Katsina
  ('AD', 9.3265, 12.3984, 3),   -- Adamawa
  ('TA', 8.8941, 11.3600, 3),   -- Taraba
  ('KB', 12.4539, 4.1975, 3),   -- Kebbi
  ('NA', 8.4988, 8.5200, 3),    -- Nasarawa
  ('KG', 7.7337, 6.6906, 3),    -- Kogi
  ('EB', 6.2649, 8.0137, 3),    -- Ebonyi
  ('ZA', 12.1584, 6.6588, 3),   -- Zamfara
  ('JI', 12.2280, 9.5616, 3),   -- Jigawa
  ('GO', 10.2897, 11.1711, 2),  -- Gombe
  ('BY', 4.7719, 6.0699, 2),    -- Bayelsa
  ('YO', 12.1844, 11.7669, 2);  -- Yobe

-- Build weighted state array for deterministic distribution (total weight = 195)
CREATE TEMP TABLE _state_slots AS
SELECT code, lat, lng, slot_num
FROM (
  SELECT code, lat, lng, generate_series(1, weight) AS slot_num FROM _states
) expanded;

CREATE TEMP TABLE _state_lookup AS
SELECT code, lat, lng, ROW_NUMBER() OVER (ORDER BY code, slot_num) - 1 AS idx
FROM _state_slots ORDER BY code, slot_num;

-- Generate 5000 contacts distributed across 5 parties and 37 states
INSERT INTO gotv_contacts (
  contact_id, party_id, phone_encrypted, phone_hash,
  full_name_encrypted, state_code, lga_code, ward_code,
  polling_unit_code, voter_status, tags, consent_id, opted_out,
  contact_count, created_at
)
SELECT
  'cnt-' || md5(n || '-contact-seed') AS contact_id,
  -- Distribute: 40% APC, 30% PDP, 15% LP, 10% NNPP, 5% ADC
  CASE
    WHEN n % 100 < 40 THEN 1
    WHEN n % 100 < 70 THEN 2
    WHEN n % 100 < 85 THEN 3
    WHEN n % 100 < 95 THEN 4
    ELSE 5
  END AS party_id,
  -- Encrypted phone (simulated hex, 32 chars)
  encode(sha256(('080' || lpad((30000000 + n)::TEXT, 8, '0'))::bytea), 'hex') AS phone_encrypted,
  -- Phone hash for dedup
  encode(sha256(('hash-' || n)::bytea), 'hex') AS phone_hash,
  -- Encrypted name
  encode(sha256(('name-' || n)::bytea), 'hex') AS full_name_encrypted,
  -- State code from weighted distribution using modular arithmetic
  sl.code AS state_code,
  -- LGA code (simulated)
  sl.code || '-LGA-' || lpad(((n % 15) + 1)::TEXT, 2, '0') AS lga_code,
  -- Ward code
  sl.code || '-W-' || lpad(((n % 20) + 1)::TEXT, 3, '0') AS ward_code,
  -- Polling unit
  sl.code || '-PU-' || lpad(((n % 50) + 1)::TEXT, 4, '0') AS polling_unit_code,
  -- Voter status distribution: 30% pledged, 20% confirmed, 5% declined, 5% unreachable, 40% unknown
  CASE
    WHEN n % 100 < 30 THEN 'pledged'
    WHEN n % 100 < 50 THEN 'confirmed'
    WHEN n % 100 < 55 THEN 'declined'
    WHEN n % 100 < 60 THEN 'unreachable'
    ELSE 'unknown'
  END AS voter_status,
  -- Tags
  CASE
    WHEN n % 7 = 0 THEN ARRAY['youth','first_time_voter']
    WHEN n % 5 = 0 THEN ARRAY['senior','needs_transport']
    WHEN n % 3 = 0 THEN ARRAY['women','market_trader']
    ELSE ARRAY['general']
  END AS tags,
  -- Consent (95% have consent)
  CASE WHEN n % 20 = 0 THEN NULL ELSE 'consent-' || md5(n::TEXT) END AS consent_id,
  -- Opted out (2%)
  n % 50 = 0 AS opted_out,
  -- Contact count (times contacted)
  (n % 8) AS contact_count,
  -- Created over last 6 months
  NOW() - ((n % 180) * interval '1 day') - ((n % 24) * interval '1 hour') AS created_at
FROM generate_series(1, 5200) n
JOIN _state_lookup sl ON sl.idx = (n * 7 + n / 37) % (SELECT COUNT(*) FROM _state_lookup)
ON CONFLICT (contact_id) DO NOTHING;

-- ─── VOLUNTEERS (500+) ─────────────────────────────────────────────────────

INSERT INTO gotv_volunteers (
  volunteer_id, party_id, full_name, phone, role,
  assigned_state, assigned_lga, assigned_ward,
  is_active, has_vehicle, vehicle_capacity,
  latitude, longitude, last_checkin_at,
  doors_knocked, calls_made, rides_given, created_at
)
SELECT
  'vol-' || md5(n || '-volunteer-seed') AS volunteer_id,
  CASE
    WHEN n % 100 < 40 THEN 1
    WHEN n % 100 < 70 THEN 2
    WHEN n % 100 < 85 THEN 3
    WHEN n % 100 < 95 THEN 4
    ELSE 5
  END AS party_id,
  -- Deterministic name selection using modular indexing
  (SELECT name FROM _first_names OFFSET (n * 3) % 144 LIMIT 1) || ' ' ||
  (SELECT name FROM _last_names OFFSET (n * 7) % 100 LIMIT 1) AS full_name,
  '080' || lpad((40000000 + n)::TEXT, 8, '0') AS phone,
  CASE
    WHEN n % 10 < 5 THEN 'canvasser'
    WHEN n % 10 < 7 THEN 'driver'
    WHEN n % 10 < 8 THEN 'caller'
    WHEN n % 10 < 9 THEN 'coordinator'
    ELSE 'team_lead'
  END AS role,
  sl.code AS assigned_state,
  sl.code || '-LGA-' || lpad((n % 15 + 1)::TEXT, 2, '0') AS assigned_lga,
  sl.code || '-W-' || lpad((n % 20 + 1)::TEXT, 3, '0') AS assigned_ward,
  -- 85% active
  n % 7 != 0 AS is_active,
  -- 30% have vehicle
  n % 3 = 0 AS has_vehicle,
  CASE WHEN n % 3 = 0 THEN 2 + (n % 4) ELSE 0 END AS vehicle_capacity,
  -- Coordinates near state center with jitter (deterministic)
  sl.lat + (((n * 13) % 100) - 50)::REAL / 333.0 AS latitude,
  sl.lng + (((n * 17) % 100) - 50)::REAL / 333.0 AS longitude,
  -- Last check-in within past 24h for active, null for inactive
  CASE WHEN n % 7 != 0 THEN NOW() - ((n % 24) * interval '1 hour') ELSE NULL END AS last_checkin_at,
  -- Performance metrics
  (n * 7 + 25) % 300 AS doors_knocked,
  (n * 3 + 15) % 150 AS calls_made,
  CASE WHEN n % 3 = 0 THEN (n % 25) ELSE 0 END AS rides_given,
  NOW() - ((n % 120) * interval '1 day') AS created_at
FROM generate_series(1, 550) n
JOIN _state_lookup sl ON sl.idx = (n * 11 + n / 19) % (SELECT COUNT(*) FROM _state_lookup)
ON CONFLICT (volunteer_id) DO NOTHING;

-- ─── CAMPAIGNS (50+) ───────────────────────────────────────────────────────

INSERT INTO gotv_campaigns (
  campaign_id, party_id, name, description, campaign_type,
  status, target_state, message_template, message_variant_b,
  ab_split_pct, total_contacts, contacts_reached, contacts_responded,
  created_by, created_at
)
SELECT
  'camp-' || md5(n || '-campaign-seed') AS campaign_id,
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END AS party_id,
  name,
  description,
  campaign_type,
  status,
  state_code,
  template_a,
  template_b,
  CASE WHEN template_b IS NOT NULL THEN 50 ELSE 100 END AS ab_split_pct,
  total_contacts,
  contacts_reached,
  (contacts_reached * 0.3)::INT AS contacts_responded,
  'coordinator-' || (n % 5 + 1) AS created_by,
  NOW() - (n * interval '3 days') AS created_at
FROM (VALUES
  (1, 'Lagos SMS Blast - Registration Reminder', 'Remind registered voters in Lagos to prepare for election day', 'sms', 'completed', 'LA', 'Remember: Feb 25 is Election Day! Your PU is {{pu}}. Come early, vote {{party}}! Reply STOP to opt out.', 'Election Day Feb 25! Find your PU at inecnigeria.org. Vote for change! Reply STOP to opt out.', 45000, 42150),
  (2, 'Kano WhatsApp - Youth Turnout', 'Target youth voters in Kano via WhatsApp', 'whatsapp', 'active', 'KN', 'Salam! Your vote matters. Election Day is {{election_date}}. Your polling unit: {{pu}}. Need a ride? Reply RIDE', NULL, 38000, 12500),
  (3, 'Rivers Door-to-Door Campaign', 'Canvassing in Port Harcourt and Obio-Akpor', 'door_to_door', 'active', 'RI', NULL, NULL, 8500, 3200),
  (4, 'FCT Push Notification - Early Voting', 'Encourage early arrival at polling units in Abuja', 'push', 'scheduled', 'FC', 'Good morning {{first_name}}! Polls open 8:30am today. Beat the queues - vote early at {{pu}}. Every vote counts!', 'Rise and shine {{first_name}}! Today is the day. Your PU: {{pu}}. Get there before 9am for shorter queues!', 22000, 0),
  (5, 'Nationwide Phone Bank', 'Phone banking campaign for swing states', 'phone_bank', 'active', NULL, NULL, NULL, 15000, 7800),
  (6, 'Oyo State USSD Campaign', 'USSD outreach for feature phone users in Oyo', 'ussd', 'completed', 'OY', 'Vote {{party}} on Feb 25! Dial *123*VOTE# for your PU location. Free!', NULL, 28000, 25600),
  (7, 'Delta Email Newsletter', 'Monthly email updates to educated voters in Delta', 'email', 'active', 'DE', 'Dear {{name}}, As election day approaches, we want to remind you of our party''s commitment to...', NULL, 12000, 9800),
  (8, 'Anambra Ride-to-Polls', 'Coordinate rides for elderly voters in Anambra', 'ride_to_polls', 'active', 'AN', NULL, NULL, 4500, 1800),
  (9, 'Twitter/X National Campaign', 'Social media awareness campaign on X/Twitter', 'twitter', 'active', NULL, 'Your vote is your voice! #NigeriaDecides2027 #VoteForChange. Find your PU at inecnigeria.org', NULL, 0, 0),
  (10, 'Facebook Targeted Ads - South-South', 'Facebook campaign targeting South-South geopolitical zone', 'facebook', 'active', NULL, 'Nigeria deserves better. On Feb 25, choose progress. #VoteRight #NigeriaDecides2027', NULL, 0, 0),
  (11, 'Instagram Stories - Gen-Z', 'Visual campaign targeting 18-25 demographic on Instagram', 'instagram', 'completed', NULL, NULL, NULL, 0, 0),
  (12, 'TikTok Voter Education', 'Short-form video campaign on TikTok', 'tiktok', 'draft', NULL, NULL, NULL, 0, 0),
  (13, 'Kaduna SMS Reminder Wave 2', 'Follow-up SMS to non-responders in Kaduna', 'sms', 'draft', 'KD', 'Hi {{first_name}}! Election Day is almost here. Will we see you at {{pu}}? Your vote is your power! Text YES to confirm.', 'Don''t miss out {{first_name}}! Feb 25 at {{pu}}. Millions are counting on you. Text YES if you''re voting!', 18000, 0),
  (14, 'Borno Security Awareness', 'SMS campaign with polling unit security info in Borno', 'sms', 'scheduled', 'BO', 'Election Day Safety: Military/Police deployed at your PU {{pu}}. Come early, stay safe. Report issues: *911#', NULL, 9500, 0),
  (15, 'WhatsApp Interactive - Ride Request', 'WhatsApp buttons for ride booking', 'whatsapp_interactive', 'active', 'LA', NULL, NULL, 15000, 8900),
  (16, 'Plateau Peace Campaign', 'SMS encouraging peaceful voting in Plateau', 'sms', 'completed', 'PL', 'Peace is our strength! Vote peacefully at {{pu}} on Feb 25. One person, one vote. {{party}} for Nigeria!', NULL, 11000, 10200),
  (17, 'Osun Women Mobilization', 'WhatsApp campaign targeting women voters in Osun', 'whatsapp', 'active', 'OS', 'Sister! Your vote matters more than ever. {{party}} stands for women''s empowerment. Vote Feb 25 at {{pu}}!', NULL, 7500, 4200),
  (18, 'Kogi Door-to-Door Rural', 'Rural canvassing in Kogi remote areas', 'door_to_door', 'paused', 'KG', NULL, NULL, 5200, 1100),
  (19, 'Cross River Email Diaspora', 'Email campaign to Cross River diaspora voters', 'email', 'draft', 'CR', 'Dear {{name}}, As a registered voter in Cross River, your voice matters even from abroad...', NULL, 3200, 0),
  (20, 'National Multi-Wave Sequence', 'SMS → WhatsApp → Phone call escalation', 'sms', 'active', NULL, 'Hi {{first_name}}! Election reminder from {{party}}. Your PU: {{pu}}. Vote on Feb 25!', 'Quick reminder {{first_name}}: Voting day is Feb 25 at {{pu}}. Make your voice heard!', 50000, 32000),
  (21, 'Edo State Mobilization', 'Full mobilization campaign in Edo', 'sms', 'active', 'ED', 'Edo! Rise up and vote for {{party}}. Feb 25 at {{pu}}. Every vote is a step toward progress!', NULL, 14000, 11500),
  (22, 'Imo WhatsApp Groups', 'WhatsApp group-based mobilization in Imo', 'whatsapp', 'completed', 'IM', 'Ndi Imo! Your vote is your weapon against bad governance. Feb 25, {{pu}}. Vote {{party}}!', NULL, 9800, 8900),
  (23, 'Ogun Push Notifications', 'Push notification reminders for Ogun voters', 'push', 'scheduled', 'OG', '🗳️ Tomorrow is Election Day! Set your alarm for 7am. Your PU: {{pu}}. {{party}} needs you!', NULL, 16500, 0),
  (24, 'Bauchi SMS Religious Leaders', 'SMS via religious leader endorsements', 'sms', 'active', 'BA', 'A message from your community: Voting is both a civic duty and a spiritual obligation. {{pu}}, Feb 25.', NULL, 7200, 5400),
  (25, 'Nasarawa USSD Poll Finder', 'USSD-based polling unit finder for Nasarawa', 'ussd', 'active', 'NA', 'Dial *555*PU# to find your polling unit. Free! Vote {{party}} on Feb 25. Your vote, your future.', NULL, 5500, 4100),
  (26, 'Adamawa Peace SMS', 'Peace messaging in conflict-prone areas', 'sms', 'completed', 'AD', 'Peace before politics! Vote calmly at {{pu}} on Feb 25. Nigeria is bigger than any party. Stay safe!', NULL, 6800, 6200),
  (27, 'Enugu Market Women', 'WhatsApp campaign targeting market women in Enugu', 'whatsapp', 'active', 'EN', 'Nne! Close shop early on Feb 25. Vote at {{pu}} before noon. Your business needs good governance!', NULL, 8300, 5100),
  (28, 'Niger State Rural SMS', 'SMS for rural voters in Niger state', 'sms', 'active', 'NI', 'Sanu da zuwa! Election day Feb 25. {{pu}} shi ne wurin zaben ku. Ku fito ku zaba!', NULL, 11000, 8200),
  (29, 'Sokoto Traditional Media', 'SMS reinforcing radio campaign in Sokoto', 'sms', 'active', 'SO', 'As heard on radio: Vote {{party}} Feb 25 at {{pu}}. The future is in your hands!', NULL, 7000, 5500),
  (30, 'Kwara WhatsApp University Alumni', 'WhatsApp campaign via university alumni networks', 'whatsapp', 'completed', 'KW', 'Fellow Kwaran! Remember your civic duty. Feb 25 at {{pu}}. Let''s show the nation what Kwara can do!', NULL, 4800, 4400),
  (31, 'Lagos Island Door-to-Door', 'Intensive canvassing in Lagos Island/Ikoyi', 'door_to_door', 'active', 'LA', NULL, NULL, 6200, 4800),
  (32, 'Katsina SMS Farmers', 'Agriculture-themed campaign for Katsina farmers', 'sms', 'scheduled', 'KT', 'Dear Farmer: {{party}} will boost agriculture! Vote Feb 25 at {{pu}}. Better fertilizer, better life!', NULL, 9000, 0),
  (33, 'Benue IDP Camp Outreach', 'Reaching displaced voters in Benue IDP camps', 'sms', 'active', 'BE', 'Your vote matters even more now. IDP voting centers confirmed. Visit {{pu}} on Feb 25. {{party}} cares!', NULL, 3200, 2800),
  (34, 'Akwa Ibom Oil Workers', 'Email campaign for oil industry workers', 'email', 'active', 'AK', 'Dear colleague, ensure your vote reflects your interests. {{party}} will protect oil workers. Feb 25 at {{pu}}.', NULL, 2800, 1900),
  (35, 'Abia Traders Association', 'SMS via traders'' associations in Abia', 'sms', 'completed', 'AB', 'Fellow trader! Market day is every day, but Feb 25 is YOUR day. Vote at {{pu}}. {{party}} for business!', NULL, 5500, 5100),
  (36, 'Zamfara Security Update', 'Security-focused messaging for Zamfara voters', 'sms', 'active', 'ZA', 'Your safety is guaranteed. Security deployed at {{pu}}. Come out and vote Feb 25. Don''t let fear win!', NULL, 4500, 3200),
  (37, 'Ebonyi Infrastructure', 'Development-themed campaign for Ebonyi', 'sms', 'draft', 'EB', 'Roads! Water! Electricity! {{party}} will deliver. Make your voice count Feb 25 at {{pu}}.', NULL, 4200, 0),
  (38, 'Kebbi Rural USSD', 'USSD for extremely rural areas in Kebbi', 'ussd', 'active', 'KB', 'Dial *123*VOTE# free. Find PU. Vote Feb 25. {{party}}.', NULL, 3800, 2900),
  (39, 'Gombe Student Voters', 'WhatsApp campaign for university students in Gombe', 'whatsapp', 'active', 'GO', 'Comrade! Don''t waste your PVC. {{pu}} needs you Feb 25. Vote for your future! {{party}}', NULL, 2500, 1800),
  (40, 'Bayelsa Creek Communities', 'SMS for riverine communities in Bayelsa', 'sms', 'active', 'BY', 'Fellow creek dweller: Boat transport arranged to {{pu}} on Feb 25. Free ride, free vote! {{party}}', NULL, 2200, 1600),
  (41, 'Yobe Farmers SMS', 'Agriculture campaign in Yobe state', 'sms', 'completed', 'YO', 'Harvest season is over, now harvest your democracy! Vote at {{pu}} Feb 25. {{party}} for farmers!', NULL, 3100, 2800),
  (42, 'Jigawa Women Empowerment', 'WhatsApp campaign for women in Jigawa', 'whatsapp', 'active', 'JI', 'Yarinya! Your vote is your power. Come to {{pu}} on Feb 25. {{party}} stands with women!', NULL, 4000, 2700),
  (43, 'Taraba Multi-ethnic Unity', 'Peace and unity messaging across ethnic lines in Taraba', 'sms', 'active', 'TA', 'We are one! Taraba''s strength is our diversity. Vote peacefully at {{pu}} Feb 25. {{party}} unites us!', NULL, 3500, 2600),
  (44, 'Ondo Teachers Campaign', 'Email targeting teachers and education workers', 'email', 'draft', 'ON', 'Dear Educator, better wages and conditions start with your vote. {{party}} at {{pu}} on Feb 25.', NULL, 2900, 0),
  (45, 'Ekiti Cultural Campaign', 'SMS leveraging Ekiti cultural pride', 'sms', 'active', 'EK', 'Ekiti kete! Fountain of knowledge, fountain of votes! {{pu}} awaits you Feb 25. Vote {{party}}!', NULL, 3800, 3200),
  (46, 'National Last-Day Reminder', 'Final day mega SMS blast to all states', 'sms', 'draft', NULL, 'TODAY IS THE DAY! Polls close 2:30pm. Get to {{pu}} NOW! Your vote + million others = CHANGE! {{party}}', 'LAST CHANCE! Polls closing soon. Rush to {{pu}}! Don''t let others decide YOUR future. VOTE NOW!', 95000, 0),
  (47, 'Facebook Live Events', 'Live event announcements on Facebook', 'facebook', 'active', NULL, 'Join our LIVE town hall tonight 8pm! Ask questions, get answers. #{{party}}Answers #NigeriaDecides2027', NULL, 0, 0),
  (48, 'WhatsApp Broadcast Lists', 'Official party broadcast channel', 'whatsapp', 'active', NULL, 'OFFICIAL: Election Day logistis confirmed. PU opens 8:30am. Bring your PVC and valid ID. {{party}} victory!', NULL, 25000, 18000),
  (49, 'SMS A/B Split Test - Language', 'Testing Pidgin vs Standard English', 'sms', 'completed', 'LA', 'Make you no forget! Feb 25 na di day. Go {{pu}} go vote. {{party}} na di movement!', 'Don''t forget! February 25 is Election Day. Visit {{pu}} to cast your vote. Choose {{party}} for progress!', 20000, 18500),
  (50, 'Door-to-Door Pledge Collection', 'Collecting voting pledges via canvassers', 'door_to_door', 'active', NULL, NULL, NULL, 30000, 15400),
  (51, 'WhatsApp Voice Note Campaign', 'Voice note campaigning via WhatsApp', 'whatsapp', 'draft', 'KN', NULL, NULL, 12000, 0),
  (52, 'USSD Voter Registration Check', 'Help unregistered voters find INEC offices', 'ussd', 'completed', NULL, 'Check your registration: Dial *565*VIN#. Not registered? Visit INEC office at {{ward}}. Free!', NULL, 8000, 7200),
  (53, 'Kano Night SMS', 'Evening SMS for Kano (respect prayer times)', 'sms', 'active', 'KN', 'Barka da dare! Quick reminder: Feb 25 election at {{pu}}. Set your alarm for 7am! {{party}} victory insha Allah!', NULL, 12500, 9800),
  (54, 'Lagos Mainland Canvass', 'Aggressive door-to-door in Lagos Mainland', 'door_to_door', 'active', 'LA', NULL, NULL, 9800, 7200),
  (55, 'Rivers Oil Community', 'SMS for oil-producing communities', 'sms', 'active', 'RI', 'Our oil, our vote! {{party}} will ensure host communities benefit. {{pu}}, Feb 25. Vote!', NULL, 6500, 5100)
) AS v(n, name, description, campaign_type, status, state_code, template_a, template_b, total_contacts, contacts_reached)
ON CONFLICT (campaign_id) DO NOTHING;

-- ─── PLEDGES (2000+) ───────────────────────────────────────────────────────

INSERT INTO gotv_pledges (
  pledge_id, party_id, contact_id, election_id, pledge_type, status,
  reminder_sent, reminder_sent_at, notes, created_at
)
SELECT
  'plg-' || md5(n || '-pledge-seed') AS pledge_id,
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END AS party_id,
  'cnt-' || md5(((n % 5000) + 1) || '-contact-seed') AS contact_id,
  1 AS election_id,
  CASE
    WHEN n % 4 = 0 THEN 'will_vote'
    WHEN n % 4 = 1 THEN 'needs_ride'
    WHEN n % 4 = 2 THEN 'needs_info'
    ELSE 'will_volunteer'
  END AS pledge_type,
  CASE
    WHEN n % 5 = 0 THEN 'fulfilled'
    WHEN n % 5 = 1 THEN 'confirmed_day_of'
    WHEN n % 5 = 2 THEN 'reminded'
    WHEN n % 5 = 3 THEN 'pledged'
    ELSE 'broken'
  END AS status,
  n % 3 = 0 AS reminder_sent,
  CASE WHEN n % 3 = 0 THEN NOW() - (random() * interval '7 days') ELSE NULL END AS reminder_sent_at,
  CASE
    WHEN n % 10 = 0 THEN 'Enthusiastic supporter, will bring family'
    WHEN n % 10 = 1 THEN 'Needs transport from remote area'
    WHEN n % 10 = 2 THEN 'First time voter, needs guidance'
    WHEN n % 10 = 3 THEN 'Willing to volunteer on election day'
    WHEN n % 10 = 4 THEN 'Senior citizen, mobility issues'
    WHEN n % 10 = 5 THEN 'Market woman, needs early morning slot'
    WHEN n % 10 = 6 THEN 'University student, organized group transport'
    WHEN n % 10 = 7 THEN 'Community leader, will mobilize ward'
    ELSE NULL
  END AS notes,
  NOW() - (random() * interval '90 days') AS created_at
FROM generate_series(1, 2500) n
ON CONFLICT (pledge_id) DO NOTHING;

-- ─── RIDE REQUESTS (800+) ──────────────────────────────────────────────────

INSERT INTO gotv_ride_requests (
  request_id, party_id, contact_id, volunteer_id,
  pickup_latitude, pickup_longitude, polling_unit_code,
  status, requested_at, matched_at, picked_up_at, dropped_off_at, distance_km
)
SELECT
  'ride-' || md5(n || '-ride-seed') AS request_id,
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END AS party_id,
  'cnt-' || md5(((n % 5000) + 1) || '-contact-seed') AS contact_id,
  CASE WHEN n % 3 != 0 THEN 'vol-' || md5(((n % 500) + 1) || '-volunteer-seed') ELSE NULL END AS volunteer_id,
  -- Pickup around Lagos/Kano/Rivers (major cities)
  CASE
    WHEN n % 4 = 0 THEN 6.45 + (random() * 0.2)
    WHEN n % 4 = 1 THEN 12.0 + (random() * 0.15)
    WHEN n % 4 = 2 THEN 4.8 + (random() * 0.1)
    ELSE 9.05 + (random() * 0.1)
  END AS pickup_latitude,
  CASE
    WHEN n % 4 = 0 THEN 3.35 + (random() * 0.2)
    WHEN n % 4 = 1 THEN 8.5 + (random() * 0.2)
    WHEN n % 4 = 2 THEN 7.0 + (random() * 0.15)
    ELSE 7.45 + (random() * 0.15)
  END AS pickup_longitude,
  'PU-' || lpad((n % 200 + 1)::TEXT, 4, '0') AS polling_unit_code,
  CASE
    WHEN n % 7 = 0 THEN 'pending'
    WHEN n % 7 = 1 THEN 'matched'
    WHEN n % 7 = 2 THEN 'en_route'
    WHEN n % 7 = 3 THEN 'picked_up'
    WHEN n % 7 = 4 THEN 'dropped_off'
    WHEN n % 7 = 5 THEN 'cancelled'
    ELSE 'no_show'
  END AS status,
  NOW() - (random() * interval '30 days') AS requested_at,
  CASE WHEN n % 7 > 0 THEN NOW() - (random() * interval '29 days') ELSE NULL END AS matched_at,
  CASE WHEN n % 7 >= 3 THEN NOW() - (random() * interval '28 days') ELSE NULL END AS picked_up_at,
  CASE WHEN n % 7 = 4 THEN NOW() - (random() * interval '27 days') ELSE NULL END AS dropped_off_at,
  1.5 + (random() * 12.0) AS distance_km
FROM generate_series(1, 900) n
ON CONFLICT (request_id) DO NOTHING;

-- ─── OUTREACH LOG (10000+) ─────────────────────────────────────────────────
-- This is the main activity table showing all communication history

INSERT INTO gotv_outreach_log (
  party_id, campaign_id, contact_id, channel, direction,
  message_variant, status, message_id, error_detail,
  latency_ms, cost_kobo, sent_at, delivered_at
)
SELECT
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END,
  'camp-' || md5(((n % 55) + 1) || '-campaign-seed'),
  'cnt-' || md5(((n % 5000) + 1) || '-contact-seed'),
  CASE
    WHEN n % 10 < 4 THEN 'sms'
    WHEN n % 10 < 6 THEN 'whatsapp'
    WHEN n % 10 < 7 THEN 'push'
    WHEN n % 10 < 8 THEN 'email'
    WHEN n % 10 < 9 THEN 'door_knock'
    ELSE 'phone_call'
  END,
  CASE WHEN n % 20 = 0 THEN 'inbound' ELSE 'outbound' END,
  CASE WHEN n % 2 = 0 THEN 'a' ELSE 'b' END,
  CASE
    WHEN n % 12 < 5 THEN 'delivered'
    WHEN n % 12 < 8 THEN 'sent'
    WHEN n % 12 < 9 THEN 'read'
    WHEN n % 12 < 10 THEN 'responded'
    WHEN n % 12 < 11 THEN 'failed'
    ELSE 'pending'
  END,
  'msg-' || md5(n::TEXT || '-outreach'),
  CASE WHEN n % 12 = 10 THEN 'Carrier rejected: number not in service' ELSE NULL END,
  50 + (random() * 2000)::INT,
  CASE
    WHEN n % 10 < 4 THEN 4  -- SMS: ₦4
    WHEN n % 10 < 6 THEN 2  -- WhatsApp: ₦2
    WHEN n % 10 < 7 THEN 1  -- Push: ₦1
    WHEN n % 10 < 8 THEN 5  -- Email: ₦5
    ELSE 0
  END,
  NOW() - (random() * interval '60 days'),
  CASE WHEN n % 12 < 5 THEN NOW() - (random() * interval '59 days') ELSE NULL END
FROM generate_series(1, 12000) n;

-- ─── DOOR KNOCKS (5000+) ───────────────────────────────────────────────────

INSERT INTO gotv_door_knocks (
  knock_id, party_id, volunteer_id, contact_id, shift_id,
  latitude, longitude, outcome, notes, speed_kmh,
  is_suspicious, knocked_at
)
SELECT
  'knk-' || md5(n || '-knock-seed') AS knock_id,
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END AS party_id,
  'vol-' || md5(((n % 300) + 1) || '-volunteer-seed') AS volunteer_id,
  CASE WHEN n % 4 != 0 THEN 'cnt-' || md5(((n % 5000) + 1) || '-contact-seed') ELSE NULL END AS contact_id,
  'shift-' || md5(((n % 200) + 1) || '-shift-seed') AS shift_id,
  -- Coordinates in Lagos, Kano, Rivers, FCT
  CASE
    WHEN n % 4 = 0 THEN 6.45 + (random() * 0.15)
    WHEN n % 4 = 1 THEN 12.0 + (random() * 0.1)
    WHEN n % 4 = 2 THEN 4.8 + (random() * 0.08)
    ELSE 9.05 + (random() * 0.08)
  END AS latitude,
  CASE
    WHEN n % 4 = 0 THEN 3.35 + (random() * 0.15)
    WHEN n % 4 = 1 THEN 8.5 + (random() * 0.15)
    WHEN n % 4 = 2 THEN 7.0 + (random() * 0.1)
    ELSE 7.45 + (random() * 0.1)
  END AS longitude,
  CASE
    WHEN n % 7 = 0 THEN 'home'
    WHEN n % 7 = 1 THEN 'not_home'
    WHEN n % 7 = 2 THEN 'refused'
    WHEN n % 7 = 3 THEN 'pledged'
    WHEN n % 7 = 4 THEN 'already_voted'
    WHEN n % 7 = 5 THEN 'moved'
    ELSE 'callback'
  END AS outcome,
  CASE
    WHEN n % 15 = 0 THEN 'Very supportive, will bring 5 family members'
    WHEN n % 15 = 1 THEN 'Undecided, needs more info about manifesto'
    WHEN n % 15 = 2 THEN 'Hostile area, not safe to return'
    WHEN n % 15 = 3 THEN 'Young voter, excited about change'
    WHEN n % 15 = 4 THEN 'Elder, needs transport assistance'
    WHEN n % 15 = 5 THEN 'Supports opposition but open to discussion'
    ELSE NULL
  END AS notes,
  -- Normal walking speed is 3-5 km/h; >10 is suspicious
  CASE WHEN n % 50 = 0 THEN 15.0 + (random() * 10) ELSE 2.0 + (random() * 4.0) END AS speed_kmh,
  -- Flag 2% as suspicious (high speed)
  n % 50 = 0 AS is_suspicious,
  NOW() - (random() * interval '45 days') AS knocked_at
FROM generate_series(1, 6000) n
ON CONFLICT (knock_id) DO NOTHING;

-- ─── SHIFTS (200+) ─────────────────────────────────────────────────────────

INSERT INTO gotv_shifts (
  shift_id, party_id, volunteer_id,
  start_lat, start_lng, end_lat, end_lng,
  started_at, ended_at
)
SELECT
  'shift-' || md5(n || '-shift-seed') AS shift_id,
  CASE WHEN n % 5 = 0 THEN 1 WHEN n % 5 = 1 THEN 2 WHEN n % 5 = 2 THEN 3 WHEN n % 5 = 3 THEN 4 ELSE 5 END AS party_id,
  'vol-' || md5(((n % 300) + 1) || '-volunteer-seed') AS volunteer_id,
  6.4 + (random() * 0.3) AS start_lat,
  3.3 + (random() * 0.3) AS start_lng,
  CASE WHEN n % 5 != 0 THEN 6.4 + (random() * 0.3) ELSE NULL END AS end_lat,
  CASE WHEN n % 5 != 0 THEN 3.3 + (random() * 0.3) ELSE NULL END AS end_lng,
  NOW() - (random() * interval '30 days') AS started_at,
  CASE WHEN n % 5 != 0 THEN NOW() - (random() * interval '29 days') ELSE NULL END AS ended_at
FROM generate_series(1, 250) n
ON CONFLICT (shift_id) DO NOTHING;

-- ─── TERRITORIES ───────────────────────────────────────────────────────────

INSERT INTO gotv_territories (territory_id, party_id, volunteer_id, ward_code, contact_count, status)
SELECT
  'ter-' || md5(n || '-territory-seed'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  'vol-' || md5(((n % 300) + 1) || '-volunteer-seed'),
  sl.code || '-W-' || lpad((n % 20 + 1)::TEXT, 3, '0'),
  50 + (n * 13) % 200,
  CASE WHEN n % 4 = 0 THEN 'completed' WHEN n % 4 = 1 THEN 'in_progress' ELSE 'assigned' END
FROM generate_series(1, 150) n
JOIN _state_lookup sl ON sl.idx = (n * 5 + 3) % (SELECT COUNT(*) FROM _state_lookup)
ON CONFLICT (territory_id) DO NOTHING;

-- ─── CAMPAIGN SEQUENCES ────────────────────────────────────────────────────

INSERT INTO gotv_segments (segment_id, party_id, name, filters)
SELECT
  'seg-' || md5(n || '-segment-seed'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  name,
  filters::JSONB
FROM (VALUES
  (1, 'Youth Voters (18-25)', '[{"field":"tags","op":"contains","value":"youth"}]'),
  (2, 'Senior Citizens', '[{"field":"tags","op":"contains","value":"senior"}]'),
  (3, 'Women Market Traders', '[{"field":"tags","op":"contains","value":"market_trader"}]'),
  (4, 'First-Time Voters', '[{"field":"tags","op":"contains","value":"first_time_voter"}]'),
  (5, 'Needs Transport', '[{"field":"tags","op":"contains","value":"needs_transport"}]'),
  (6, 'Lagos Mainland', '[{"field":"state_code","op":"eq","value":"LA"},{"field":"lga_code","op":"contains","value":"Mainland"}]'),
  (7, 'Kano Urban', '[{"field":"state_code","op":"eq","value":"KN"}]'),
  (8, 'Pledged but Not Confirmed', '[{"field":"voter_status","op":"eq","value":"pledged"}]'),
  (9, 'Never Contacted', '[{"field":"contact_count","op":"eq","value":"0"}]'),
  (10, 'High-Value Mobilizers', '[{"field":"tags","op":"contains","value":"community_leader"}]'),
  (11, 'South-South Zone', '[{"field":"state_code","op":"in","value":"RI,DE,BY,AK,CR,ED"}]'),
  (12, 'North-West Zone', '[{"field":"state_code","op":"in","value":"KN,KD,KT,SO,ZA,KB,JI"}]'),
  (13, 'Unreachable - Retry', '[{"field":"voter_status","op":"eq","value":"unreachable"}]'),
  (14, 'Opted-In with Consent', '[{"field":"consent_id","op":"not_null","value":""}]'),
  (15, 'Rural Communities', '[{"field":"lga_code","op":"contains","value":"Rural"}]')
) AS v(n, name, filters)
ON CONFLICT (segment_id) DO NOTHING;

-- ─── CAMPAIGN SEQUENCES (Multi-wave) ───────────────────────────────────────

INSERT INTO gotv_campaign_sequences (sequence_id, party_id, name, waves, status)
VALUES
  ('seq-001', 1, 'APC 3-Wave Mobilization', '[{"wave":1,"channel":"sms","delay_hours":0,"template":"Initial reminder"},{"wave":2,"channel":"whatsapp","delay_hours":72,"template":"Follow-up with ride offer"},{"wave":3,"channel":"phone_call","delay_hours":168,"template":"Personal call from coordinator"}]', 'active'),
  ('seq-002', 1, 'APC Election Eve Push', '[{"wave":1,"channel":"sms","delay_hours":0,"template":"Tomorrow is the day!"},{"wave":2,"channel":"push","delay_hours":12,"template":"Morning alarm reminder"}]', 'active'),
  ('seq-003', 2, 'PDP Youth Engagement', '[{"wave":1,"channel":"whatsapp","delay_hours":0,"template":"Welcome to PDP youth network"},{"wave":2,"channel":"twitter","delay_hours":24,"template":"Share your pledge"},{"wave":3,"channel":"sms","delay_hours":120,"template":"Election reminder"}]', 'active'),
  ('seq-004', 2, 'PDP Door-to-Phone Escalation', '[{"wave":1,"channel":"door_to_door","delay_hours":0,"template":"Field visit"},{"wave":2,"channel":"phone_call","delay_hours":48,"template":"Follow-up call if not home"}]', 'draft'),
  ('seq-005', 3, 'LP Digital Blitz', '[{"wave":1,"channel":"whatsapp","delay_hours":0,"template":"LP vision message"},{"wave":2,"channel":"email","delay_hours":24,"template":"Detailed manifesto"},{"wave":3,"channel":"push","delay_hours":168,"template":"Vote reminder"}]', 'active'),
  ('seq-006', 3, 'LP Market Women Outreach', '[{"wave":1,"channel":"sms","delay_hours":0,"template":"Pidgin friendly intro"},{"wave":2,"channel":"whatsapp","delay_hours":48,"template":"Visual infographic"},{"wave":3,"channel":"ussd","delay_hours":120,"template":"PU finder dial code"}]', 'active'),
  ('seq-007', 4, 'NNPP Northern Strategy', '[{"wave":1,"channel":"sms","delay_hours":0,"template":"Hausa greeting"},{"wave":2,"channel":"ussd","delay_hours":72,"template":"PU confirmation"},{"wave":3,"channel":"phone_call","delay_hours":144,"template":"Personal reminder in Hausa"}]', 'draft'),
  ('seq-008', 1, 'APC Ride Coordination', '[{"wave":1,"channel":"sms","delay_hours":0,"template":"Do you need a ride?"},{"wave":2,"channel":"whatsapp_interactive","delay_hours":24,"template":"Book ride button"},{"wave":3,"channel":"push","delay_hours":168,"template":"Your ride is confirmed!"}]', 'active')
ON CONFLICT (sequence_id) DO NOTHING;

-- ─── AI VARIANTS ───────────────────────────────────────────────────────────

INSERT INTO gotv_ai_variants (variant_id, party_id, base_message, variant_text, target_state, channel, variant_index)
VALUES
  ('aiv-001', 1, 'Remember to vote on Election Day!', 'Omo! Feb 25 na the day o! Make you no forget to go {{pu}} vote. Na your right!', 'LA', 'sms', 1),
  ('aiv-002', 1, 'Remember to vote on Election Day!', 'My brother/sister, February 25 your PU dey wait you. Come early, beat queue. God bless!', 'LA', 'sms', 2),
  ('aiv-003', 1, 'Remember to vote on Election Day!', 'Salam alaykum! Don''t forget Feb 25 at {{pu}}. Your one vote can change everything. Insha Allah!', 'KN', 'sms', 1),
  ('aiv-004', 2, 'Your vote matters for the future', 'Ndi be anyi! Otu aka gi nwere ike igbanwe Nigeria. Feb 25, {{pu}}. Sopu!', 'AN', 'whatsapp', 1),
  ('aiv-005', 2, 'Your vote matters for the future', 'Bros, your vote na power wey nobody fit take. Use am Feb 25 for {{pu}}!', 'RI', 'whatsapp', 2),
  ('aiv-006', 3, 'Labour Party stands for workers', 'Fellow worker! The hammer and the vote are your tools. Strike on Feb 25 at {{pu}}!', NULL, 'sms', 1),
  ('aiv-007', 3, 'Labour Party stands for workers', 'Minimum wage ₦70,000 is just the beginning. Vote LP at {{pu}} Feb 25 for real change!', NULL, 'sms', 2),
  ('aiv-008', 1, 'Need a ride to your polling unit?', 'No wahala! Free ride dey available to {{pu}}. Just reply RIDE or call 0800-GOTV-APC', 'LA', 'whatsapp', 1),
  ('aiv-009', 2, 'Early morning voting is easier', 'Beat the sun! Get to {{pu}} by 8:30am. Short queues, quick vote, back to business by 10am!', NULL, 'push', 1),
  ('aiv-010', 1, 'Community leaders endorse voting', 'Your Oba/Emir/Bishop says: Voting is a duty! Honor your community at {{pu}} on Feb 25.', NULL, 'sms', 1),
  ('aiv-011', 4, 'NNPP for Northern development', 'Arewa ta tashi! NNPP ce jam''iyyar ci gaba. {{pu}} a ranar 25 ga Fabrairu. Ku fito ku zaba!', 'KN', 'sms', 1),
  ('aiv-012', 5, 'ADC for change', 'True change starts with your vote. ADC at {{pu}}, February 25. Be the difference!', NULL, 'email', 1),
  ('aiv-013', 1, 'Thank you for pledging!', 'Thank you {{first_name}}! Your pledge is noted. See you at {{pu}} on Feb 25! APC appreciates you!', NULL, 'whatsapp', 1),
  ('aiv-014', 2, 'Reminder: You pledged to vote', '{{first_name}}, you promised! Don''t break am o. {{pu}} awaits you Feb 25. PDP counting on you!', NULL, 'sms', 1),
  ('aiv-015', 1, 'Election Day morning blast', 'GOOD MORNING {{first_name}}! 🗳️ TODAY IS THE DAY! {{pu}} opens 8:30am. Every single vote counts. See you there!', NULL, 'push', 1)
ON CONFLICT (variant_id) DO NOTHING;

-- ─── CHALLENGES (Gamification) ─────────────────────────────────────────────

INSERT INTO gotv_challenges (challenge_id, party_id, name, target_metric, target_value, reward_description, starts_at, ends_at)
VALUES
  ('chal-001', 1, 'Door Warrior - 100 Doors', 'doors_knocked', 100, '₦5,000 airtime + Door Warrior badge', NOW() - interval '14 days', NOW() + interval '14 days'),
  ('chal-002', 1, 'Pledge Master - 50 Pledges', 'pledges_collected', 50, '₦10,000 airtime + Pledge Master badge', NOW() - interval '7 days', NOW() + interval '21 days'),
  ('chal-003', 2, 'Road Captain - 20 Rides', 'rides_given', 20, '₦15,000 fuel voucher + Road Captain badge', NOW() - interval '3 days', NOW() + interval '25 days'),
  ('chal-004', 2, 'Phone Banker Elite - 200 Calls', 'calls_made', 200, '₦8,000 airtime + Phone Banker Elite badge', NOW() - interval '10 days', NOW() + interval '18 days'),
  ('chal-005', 3, 'Community Champion - 30 Referrals', 'referrals', 30, 'Party merchandise kit + Community Champion badge', NOW() - interval '5 days', NOW() + interval '23 days'),
  ('chal-006', 1, 'Early Bird - 5 Day Streak', 'consecutive_days', 5, '₦3,000 airtime + Early Bird badge', NOW() - interval '2 days', NOW() + interval '5 days'),
  ('chal-007', 3, 'Social Media Star - 10 Shares', 'social_shares', 10, 'Party branded cap + Social Star badge', NOW() - interval '7 days', NOW() + interval '14 days'),
  ('chal-008', 4, 'Northern Pioneer - 50 Contacts', 'contacts_reached', 50, '₦7,000 airtime', NOW() - interval '5 days', NOW() + interval '20 days'),
  ('chal-009', 1, 'State Champion - Top 3 Statewide', 'state_rank', 3, '₦50,000 + State Champion plaque', NOW() - interval '14 days', NOW() + interval '14 days'),
  ('chal-010', 2, 'Night Owl Caller - 50 Evening Calls', 'evening_calls', 50, '₦5,000 data bundle', NOW() - interval '3 days', NOW() + interval '18 days')
ON CONFLICT (challenge_id) DO NOTHING;

-- ─── FIELD REPORTS ─────────────────────────────────────────────────────────

INSERT INTO gotv_field_reports (report_id, party_id, issue_type, source, ward_code, phone, description, latitude, longitude, resolved)
SELECT
  'rpt-' || md5(n || '-report-seed'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  issue_type,
  source,
  ward_code,
  '080' || lpad((50000000 + n)::TEXT, 8, '0'),
  description,
  lat,
  lng,
  n % 3 = 0
FROM (VALUES
  (1, 'intimidation', 'canvasser', 'LA-W-001', 'Thugs threatening voters near Ikoyi PU', 6.45, 3.42),
  (2, 'logistics', 'coordinator', 'KN-W-005', 'Insufficient ballot papers at Nassarawa GRA PU', 12.01, 8.53),
  (3, 'violence', 'observer', 'RI-W-003', 'Physical altercation between party agents at Obio-Akpor', 4.82, 7.03),
  (4, 'fraud', 'canvasser', 'DE-W-008', 'Suspected ballot stuffing witnessed at Warri South PU-12', 5.52, 5.73),
  (5, 'logistics', 'voter', 'FC-W-002', 'BVAS machine not working at Area Council Secretariat', 9.06, 7.49),
  (6, 'intimidation', 'phone', 'OY-W-011', 'Military presence excessive, voters afraid to approach PU', 7.38, 3.93),
  (7, 'infrastructure', 'canvasser', 'AN-W-004', 'Flooded access road to Onitsha PU, voters cannot reach', 6.14, 6.78),
  (8, 'logistics', 'coordinator', 'ED-W-006', 'INEC officials arrived 3 hours late at Ring Road PU', 6.33, 5.62),
  (9, 'fraud', 'observer', 'KD-W-009', 'Underage voting observed at Zaria Central PU', 11.07, 7.72),
  (10, 'violence', 'phone', 'BO-W-001', 'Gunshots heard near Maiduguri Metropolitan PU', 11.85, 13.15),
  (11, 'infrastructure', 'canvasser', 'EN-W-007', 'No electricity at counting center, results delayed', 6.46, 7.55),
  (12, 'logistics', 'voter', 'LA-W-012', 'Queue too long, 4+ hour wait at Surulere PU', 6.50, 3.35),
  (13, 'intimidation', 'coordinator', 'KN-W-015', 'Party agents denying opposition voters access', 12.04, 8.55),
  (14, 'fraud', 'canvasser', 'RI-W-002', 'Results sheet tampered with before upload at Eleme', 4.77, 7.10),
  (15, 'infrastructure', 'observer', 'PL-W-003', 'PU relocated without notice, voters confused', 9.92, 8.89),
  (16, 'logistics', 'phone', 'OG-W-004', 'Sensitive materials arrived damaged at Abeokuta PU', 7.16, 3.35),
  (17, 'violence', 'canvasser', 'BE-W-006', 'Farmers-herders clash blocking access to rural PU', 7.73, 8.52),
  (18, 'fraud', 'observer', 'KT-W-008', 'Vote buying observed - ₦10,000 per voter at Katsina Central', 12.99, 7.60),
  (19, 'logistics', 'coordinator', 'BA-W-002', 'Insufficient security personnel at Bauchi Township PU', 10.31, 9.84),
  (20, 'intimidation', 'voter', 'AD-W-005', 'Armed groups preventing women from voting in Yola South', 9.22, 12.48),
  (21, 'infrastructure', 'canvasser', 'OS-W-009', 'Bridge collapsed blocking access to 3 polling units', 7.77, 4.55),
  (22, 'logistics', 'phone', 'IM-W-003', 'BVAS rejected valid PVCs, 50+ voters turned away', 5.48, 7.03),
  (23, 'fraud', 'coordinator', 'LA-W-020', 'Ghost voting at night after official hours in Alimosho', 6.61, 3.28),
  (24, 'violence', 'observer', 'KG-W-001', 'Cult groups disrupting voting process in Lokoja', 7.80, 6.73),
  (25, 'infrastructure', 'voter', 'CR-W-004', 'No shade structures, elderly collapsing in 40°C heat at Calabar', 4.95, 8.32),
  (26, 'logistics', 'canvasser', 'NI-W-007', 'Wrong ballot papers delivered to Niger East constituency', 9.60, 6.55),
  (27, 'intimidation', 'phone', 'SO-W-002', 'Traditional rulers openly coercing voters at Sokoto South', 13.06, 5.24),
  (28, 'fraud', 'observer', 'EB-W-005', 'Multiple voting by same individuals using different PVCs', 6.26, 8.01),
  (29, 'violence', 'coordinator', 'ZA-W-003', 'Bandits attacked voters traveling to remote PU in Zamfara', 12.16, 6.65),
  (30, 'logistics', 'canvasser', 'AK-W-006', 'Boat transport promised by INEC for riverine PU never arrived', 5.01, 7.85),
  (31, 'infrastructure', 'voter', 'BY-W-001', 'Entire PU submerged in flood water in Yenagoa', 4.93, 6.26),
  (32, 'fraud', 'phone', 'JI-W-004', 'INEC ad-hoc staff pre-thumbprinting ballots at Dutse', 11.76, 9.35),
  (33, 'logistics', 'coordinator', 'GO-W-002', 'INEC staff absent from 5 polling units in Gombe LGA', 10.29, 11.17),
  (34, 'intimidation', 'observer', 'TA-W-003', 'Ethnic militias blocking non-indigenes from voting in Jalingo', 8.89, 11.36),
  (35, 'infrastructure', 'canvasser', 'KB-W-005', 'Network coverage zero - electronic transmission impossible', 12.45, 4.19)
) AS v(n, issue_type, source, ward_code, description, lat, lng)
ON CONFLICT (report_id) DO NOTHING;

-- ─── VOICE CALLS ───────────────────────────────────────────────────────────

INSERT INTO gotv_voice_calls (call_id, campaign_id, contact_id, party_id, provider, phone_number, status, duration_seconds, outcome)
SELECT
  'call-' || md5(n || '-call-seed'),
  'camp-' || md5(((n % 10) + 1) || '-campaign-seed'),
  'cnt-' || md5(((n % 5000) + 1) || '-contact-seed'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  CASE WHEN n % 2 = 0 THEN 'africas_talking' ELSE 'twilio' END,
  '080' || lpad((60000000 + n)::TEXT, 8, '0'),
  CASE
    WHEN n % 5 = 0 THEN 'completed'
    WHEN n % 5 = 1 THEN 'no_answer'
    WHEN n % 5 = 2 THEN 'busy'
    WHEN n % 5 = 3 THEN 'voicemail'
    ELSE 'failed'
  END,
  CASE WHEN n % 5 = 0 THEN 30 + (random() * 180)::INT ELSE 0 END,
  CASE
    WHEN n % 5 = 0 AND n % 3 = 0 THEN 'pledged'
    WHEN n % 5 = 0 AND n % 3 = 1 THEN 'already_voted'
    WHEN n % 5 = 0 THEN 'needs_info'
    ELSE ''
  END
FROM generate_series(1, 300) n
ON CONFLICT (call_id) DO NOTHING;

-- ─── ALLIANCES ─────────────────────────────────────────────────────────────

INSERT INTO gotv_alliances (grant_id, grantor_party_id, grantee_party_id, resource_type, ward_code, expires_at)
VALUES
  ('ally-001', 1, 3, 'ride_sharing', 'LA-W-005', NOW() + interval '30 days'),
  ('ally-002', 1, 3, 'ride_sharing', 'LA-W-012', NOW() + interval '30 days'),
  ('ally-003', 2, 4, 'ride_sharing', 'KN-W-008', NOW() + interval '30 days'),
  ('ally-004', 3, 1, 'contact_sharing', 'FC-W-002', NOW() + interval '14 days'),
  ('ally-005', 1, 5, 'ride_sharing', 'OY-W-003', NOW() + interval '30 days')
ON CONFLICT (grant_id) DO NOTHING;

-- ─── PLEDGE HASHES (Blockchain verification) ───────────────────────────────

INSERT INTO gotv_pledge_hashes (hash, party_id, election_id, ward_code, verified)
SELECT
  encode(sha256(('pledge-hash-' || n)::bytea), 'hex'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  1,
  sl.code || '-W-' || lpad((n % 20 + 1)::TEXT, 3, '0'),
  n % 4 = 0
FROM generate_series(1, 500) n
JOIN _state_lookup sl ON sl.idx = (n * 3 + 7) % (SELECT COUNT(*) FROM _state_lookup)
ON CONFLICT (hash) DO NOTHING;

-- ─── MOBILE USERS ──────────────────────────────────────────────────────────

INSERT INTO gotv_mobile_users (
  user_id, party_id, phone_hash, phone_encrypted, display_name,
  volunteer_id, role, is_active, last_login_at, device_info
)
SELECT
  'mu-' || md5(n || '-mobile-user-seed'),
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  encode(sha256(('mobile-phone-' || n)::bytea), 'hex'),
  encode(sha256(('mobile-enc-' || n)::bytea), 'hex'),
  (SELECT name FROM _first_names OFFSET (n * 5) % 144 LIMIT 1) || ' ' ||
  (SELECT name FROM _last_names OFFSET (n * 11) % 100 LIMIT 1),
  'vol-' || md5(((n % 300) + 1) || '-volunteer-seed'),
  CASE WHEN n % 4 = 0 THEN 'canvasser' WHEN n % 4 = 1 THEN 'driver' WHEN n % 4 = 2 THEN 'coordinator' ELSE 'caller' END,
  n % 8 != 0,
  NOW() - ((n % 7) * interval '1 day') - ((n % 12) * interval '1 hour'),
  ('{"platform":"' || CASE WHEN n % 3 = 0 THEN 'android' WHEN n % 3 = 1 THEN 'ios' ELSE 'android' END || '","version":"' || CASE WHEN n % 2 = 0 THEN '2.1.0' ELSE '2.0.5' END || '","model":"' || CASE WHEN n % 5 = 0 THEN 'Samsung Galaxy A54' WHEN n % 5 = 1 THEN 'iPhone 13' WHEN n % 5 = 2 THEN 'Tecno Spark 10' WHEN n % 5 = 3 THEN 'Infinix Hot 30' ELSE 'Xiaomi Redmi Note 12' END || '"}')::JSONB
FROM generate_series(1, 200) n
ON CONFLICT (user_id) DO NOTHING;

-- ─── AUDIT LOG (Recent Activity) ──────────────────────────────────────────

INSERT INTO gotv_audit_log (party_id, actor, action, resource_type, resource_id, details, ip_address, created_at)
SELECT
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  'coordinator-' || (n % 5 + 1),
  action,
  resource_type,
  resource_id,
  details::JSONB,
  '102.89.' || (n % 255) || '.' || ((n * 7) % 255),
  NOW() - (random() * interval '30 days')
FROM (
  SELECT n,
    CASE
      WHEN n % 8 = 0 THEN 'campaign.launched'
      WHEN n % 8 = 1 THEN 'contact.imported'
      WHEN n % 8 = 2 THEN 'volunteer.registered'
      WHEN n % 8 = 3 THEN 'ride.matched'
      WHEN n % 8 = 4 THEN 'outreach.sent'
      WHEN n % 8 = 5 THEN 'campaign.paused'
      WHEN n % 8 = 6 THEN 'contact.opted_out'
      ELSE 'volunteer.deactivated'
    END AS action,
    CASE
      WHEN n % 8 < 2 THEN 'campaign'
      WHEN n % 8 < 4 THEN 'volunteer'
      WHEN n % 8 < 6 THEN 'outreach'
      ELSE 'contact'
    END AS resource_type,
    'res-' || md5(n::TEXT) AS resource_id,
    CASE
      WHEN n % 8 = 0 THEN '{"campaign_name":"Wave ' || n || '","contacts_targeted":' || (1000 + n * 100) || '}'
      WHEN n % 8 = 1 THEN '{"import_count":' || (50 + n * 10) || ',"source":"csv_upload"}'
      WHEN n % 8 = 2 THEN '{"role":"canvasser","state":"LA"}'
      WHEN n % 8 = 3 THEN '{"distance_km":' || (2 + n % 10) || ',"volunteer":"vol-' || (n % 100) || '"}'
      ELSE '{"channel":"sms","count":' || (10 + n * 5) || '}'
    END AS details
  FROM generate_series(1, 500) n
) sub;

-- ─── DEAD LETTER QUEUE (Failed Messages) ──────────────────────────────────

INSERT INTO gotv_dead_letter_queue (party_id, campaign_id, contact_id, channel, error_detail, message_body, phone_encrypted, retry_count, resolved)
SELECT
  CASE WHEN n % 3 = 0 THEN 1 WHEN n % 3 = 1 THEN 2 ELSE 3 END,
  'camp-' || md5(((n % 20) + 1) || '-campaign-seed'),
  'cnt-' || md5(((n % 5000) + 1) || '-contact-seed'),
  CASE WHEN n % 3 = 0 THEN 'sms' WHEN n % 3 = 1 THEN 'whatsapp' ELSE 'email' END,
  CASE
    WHEN n % 5 = 0 THEN 'Carrier rejected: invalid number format'
    WHEN n % 5 = 1 THEN 'WhatsApp: outside 24h window, template required'
    WHEN n % 5 = 2 THEN 'Network timeout after 10s'
    WHEN n % 5 = 3 THEN 'Rate limited by provider (429)'
    ELSE 'Recipient phone switched off'
  END,
  'Message body for contact ' || n,
  encode(sha256(('dlq-phone-' || n)::bytea), 'hex'),
  3,
  n % 5 = 0
FROM generate_series(1, 100) n;

-- ─── IMPORT LOG ────────────────────────────────────────────────────────────

INSERT INTO gotv_import_log (party_id, import_count, imported_at)
VALUES
  (1, 2500, NOW() - interval '45 days'),
  (1, 1800, NOW() - interval '30 days'),
  (1, 950, NOW() - interval '15 days'),
  (2, 2200, NOW() - interval '42 days'),
  (2, 1500, NOW() - interval '28 days'),
  (3, 1200, NOW() - interval '38 days'),
  (3, 800, NOW() - interval '20 days'),
  (4, 600, NOW() - interval '35 days'),
  (5, 400, NOW() - interval '32 days');

-- ─── Cleanup temp tables ───────────────────────────────────────────────────

DROP TABLE IF EXISTS _first_names;
DROP TABLE IF EXISTS _last_names;
DROP TABLE IF EXISTS _states;
DROP TABLE IF EXISTS _state_slots;
DROP TABLE IF EXISTS _state_lookup;

-- ─── Summary ───────────────────────────────────────────────────────────────

DO $$
DECLARE
  cnt RECORD;
BEGIN
  RAISE NOTICE '═══════════════════════════════════════════════════════════════';
  RAISE NOTICE ' GOTV Seed Data Summary';
  RAISE NOTICE '═══════════════════════════════════════════════════════════════';
  SELECT COUNT(*) AS c INTO cnt FROM gotv_contacts;
  RAISE NOTICE ' Contacts:         %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_volunteers;
  RAISE NOTICE ' Volunteers:       %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_campaigns;
  RAISE NOTICE ' Campaigns:        %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_pledges;
  RAISE NOTICE ' Pledges:          %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_ride_requests;
  RAISE NOTICE ' Ride Requests:    %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_outreach_log;
  RAISE NOTICE ' Outreach Logs:    %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_door_knocks;
  RAISE NOTICE ' Door Knocks:      %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_shifts;
  RAISE NOTICE ' Shifts:           %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_territories;
  RAISE NOTICE ' Territories:      %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_segments;
  RAISE NOTICE ' Segments:         %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_campaign_sequences;
  RAISE NOTICE ' Sequences:        %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_ai_variants;
  RAISE NOTICE ' AI Variants:      %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_challenges;
  RAISE NOTICE ' Challenges:       %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_field_reports;
  RAISE NOTICE ' Field Reports:    %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_voice_calls;
  RAISE NOTICE ' Voice Calls:      %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_alliances;
  RAISE NOTICE ' Alliances:        %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_pledge_hashes;
  RAISE NOTICE ' Pledge Hashes:    %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_mobile_users;
  RAISE NOTICE ' Mobile Users:     %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_audit_log;
  RAISE NOTICE ' Audit Logs:       %', cnt.c;
  SELECT COUNT(*) AS c INTO cnt FROM gotv_dead_letter_queue;
  RAISE NOTICE ' Dead Letter Queue:%', cnt.c;
  RAISE NOTICE '═══════════════════════════════════════════════════════════════';
END $$;

COMMIT;
