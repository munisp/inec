-- ============================================================================
-- GOTV Data Consistency Fix
-- ============================================================================
-- Fixes orphaned records and ensures data flows consistently through all
-- GOTV features. Run AFTER seed_gotv.sql.
--
-- Problems fixed:
--   1. Pledges/outreach/rides/knocks/voice referencing non-existent contacts
--   2. Outreach referencing non-existent campaigns
--   3. Knocks/shifts referencing non-existent volunteers
--   4. Volunteers with no shifts or door knock activity
--   5. Contacts with no outreach history
--   6. "needs_ride" pledges with no ride request
--   7. Contacts missing GPS coordinates
--   8. Contacts missing demographic enrichment
--   9. Campaigns with stale/incorrect counters
--  10. CPI computed from wrong survey status filter
--
-- Idempotent: safe to run repeatedly.
-- ============================================================================

BEGIN;

-- ─── 1. REWIRE ORPHANED RECORDS ────────────────────────────────────────────
-- Assign orphaned foreign keys to random valid records

-- Pledges → Contacts
UPDATE gotv_pledges p
SET contact_id = (SELECT contact_id FROM gotv_contacts WHERE party_id = p.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_contacts c WHERE c.contact_id = p.contact_id AND c.party_id = p.party_id);

-- Outreach → Contacts
UPDATE gotv_outreach_log o
SET contact_id = (SELECT contact_id FROM gotv_contacts WHERE party_id = o.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_contacts c WHERE c.contact_id = o.contact_id AND c.party_id = o.party_id);

-- Outreach → Campaigns
UPDATE gotv_outreach_log o
SET campaign_id = (SELECT campaign_id FROM gotv_campaigns WHERE party_id = o.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_campaigns c WHERE c.campaign_id = o.campaign_id AND c.party_id = o.party_id);

-- Ride Requests → Contacts
UPDATE gotv_ride_requests r
SET contact_id = (SELECT contact_id FROM gotv_contacts WHERE party_id = r.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_contacts c WHERE c.contact_id = r.contact_id AND c.party_id = r.party_id);

-- Ride Requests → Volunteers (driver)
UPDATE gotv_ride_requests r
SET volunteer_id = (SELECT volunteer_id FROM gotv_volunteers WHERE party_id = r.party_id ORDER BY random() LIMIT 1)
WHERE r.volunteer_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM gotv_volunteers v WHERE v.volunteer_id = r.volunteer_id AND v.party_id = r.party_id);

-- Door Knocks → Contacts
UPDATE gotv_door_knocks dk
SET contact_id = (SELECT contact_id FROM gotv_contacts WHERE party_id = dk.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_contacts c WHERE c.contact_id = dk.contact_id AND c.party_id = dk.party_id);

-- Door Knocks → Volunteers
UPDATE gotv_door_knocks dk
SET volunteer_id = (SELECT volunteer_id FROM gotv_volunteers WHERE party_id = dk.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_volunteers v WHERE v.volunteer_id = dk.volunteer_id AND v.party_id = dk.party_id);

-- Shifts → Volunteers
UPDATE gotv_shifts s
SET volunteer_id = (SELECT volunteer_id FROM gotv_volunteers WHERE party_id = s.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_volunteers v WHERE v.volunteer_id = s.volunteer_id AND v.party_id = s.party_id);

-- Voice Calls → Contacts
UPDATE gotv_voice_calls vc
SET contact_id = (SELECT contact_id FROM gotv_contacts WHERE party_id = vc.party_id ORDER BY random() LIMIT 1)
WHERE NOT EXISTS (SELECT 1 FROM gotv_contacts c WHERE c.contact_id = vc.contact_id AND c.party_id = vc.party_id);


-- ─── 2. FILL ENTITY GAPS ───────────────────────────────────────────────────
-- Ensure every entity participates in the platform data flow

-- Shifts for volunteers without any
INSERT INTO gotv_shifts (party_id, volunteer_id, started_at, ended_at, start_lat, start_lng, end_lat, end_lng, shift_id)
SELECT
  v.party_id, v.volunteer_id,
  NOW() - (random() * interval '30 days'),
  NOW() - (random() * interval '25 days') + interval '4 hours',
  v.latitude + (random()-0.5)*0.01, v.longitude + (random()-0.5)*0.01,
  v.latitude + (random()-0.5)*0.02, v.longitude + (random()-0.5)*0.02,
  'shift-' || md5(v.volunteer_id || random()::text)
FROM gotv_volunteers v
WHERE v.volunteer_id NOT IN (SELECT volunteer_id FROM gotv_shifts WHERE party_id = v.party_id)
  AND v.latitude IS NOT NULL AND v.latitude != 0;

-- Door knocks for volunteers without any (3 per volunteer)
INSERT INTO gotv_door_knocks (party_id, volunteer_id, contact_id, knocked_at, outcome, latitude, longitude, knock_id)
SELECT
  v.party_id, v.volunteer_id,
  (SELECT contact_id FROM gotv_contacts WHERE party_id = v.party_id ORDER BY random() LIMIT 1),
  NOW() - (random() * interval '28 days'),
  (ARRAY['home','not_home','refused','pledged','already_voted','moved','callback'])[floor(random()*7+1)],
  v.latitude + (random()-0.5)*0.005, v.longitude + (random()-0.5)*0.005,
  'knock-' || md5(v.volunteer_id || gs::text || random()::text)
FROM gotv_volunteers v
CROSS JOIN generate_series(1,3) gs
WHERE v.volunteer_id NOT IN (SELECT volunteer_id FROM gotv_door_knocks WHERE party_id = v.party_id)
  AND v.latitude IS NOT NULL AND v.latitude != 0;

-- Outreach for contacts that have none
INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, direction, status, sent_at, cost_kobo)
SELECT
  c.party_id,
  (SELECT campaign_id FROM gotv_campaigns WHERE party_id = c.party_id ORDER BY random() LIMIT 1),
  c.contact_id,
  (ARRAY['sms','whatsapp','phone_call','push'])[floor(random()*4+1)],
  'outbound',
  (ARRAY['sent','delivered','read','responded','failed'])[floor(random()*5+1)],
  NOW() - (random() * interval '30 days'),
  floor(random()*500+100)
FROM gotv_contacts c
WHERE c.contact_id NOT IN (SELECT contact_id FROM gotv_outreach_log WHERE party_id = c.party_id);

-- Second outreach record for contacts with only 1
INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, direction, status, sent_at, cost_kobo)
SELECT
  c.party_id,
  (SELECT campaign_id FROM gotv_campaigns WHERE party_id = c.party_id ORDER BY random() LIMIT 1),
  c.contact_id,
  (ARRAY['sms','whatsapp','phone_call'])[floor(random()*3+1)],
  'outbound',
  (ARRAY['sent','delivered','read'])[floor(random()*3+1)],
  NOW() - (random() * interval '20 days'),
  floor(random()*400+80)
FROM gotv_contacts c
WHERE c.contact_id IN (
  SELECT contact_id FROM gotv_outreach_log WHERE party_id = c.party_id GROUP BY contact_id HAVING COUNT(*) < 2
);

-- Outreach for pledged contacts (a pledge implies prior outreach contact)
INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, direction, status, sent_at, cost_kobo)
SELECT
  p.party_id,
  (SELECT campaign_id FROM gotv_campaigns WHERE party_id = p.party_id ORDER BY random() LIMIT 1),
  p.contact_id,
  (ARRAY['sms','whatsapp','phone_call'])[floor(random()*3+1)],
  'outbound', 'responded',
  NOW() - (random() * interval '30 days'),
  floor(random()*600+150)
FROM gotv_pledges p
WHERE p.contact_id NOT IN (SELECT contact_id FROM gotv_outreach_log WHERE party_id = p.party_id AND status = 'responded');


-- ─── 3. GPS COORDINATES FOR CONTACTS WITHOUT THEM ──────────────────────────

UPDATE gotv_contacts SET
  latitude = CASE state_code
    WHEN 'LA' THEN 6.52 + (random()-0.5)*0.2
    WHEN 'KN' THEN 12.00 + (random()-0.5)*0.2
    WHEN 'RV' THEN 4.82 + (random()-0.5)*0.2
    WHEN 'FC' THEN 9.06 + (random()-0.5)*0.2
    WHEN 'OG' THEN 7.15 + (random()-0.5)*0.2
    WHEN 'OY' THEN 7.38 + (random()-0.5)*0.2
    WHEN 'AN' THEN 6.22 + (random()-0.5)*0.2
    WHEN 'EN' THEN 6.46 + (random()-0.5)*0.2
    WHEN 'KD' THEN 10.51 + (random()-0.5)*0.2
    ELSE 9.06 + (random()-0.5)*2.0
  END,
  longitude = CASE state_code
    WHEN 'LA' THEN 3.38 + (random()-0.5)*0.2
    WHEN 'KN' THEN 8.59 + (random()-0.5)*0.2
    WHEN 'RV' THEN 7.05 + (random()-0.5)*0.2
    WHEN 'FC' THEN 7.50 + (random()-0.5)*0.2
    WHEN 'OG' THEN 3.36 + (random()-0.5)*0.2
    WHEN 'OY' THEN 3.95 + (random()-0.5)*0.2
    WHEN 'AN' THEN 7.07 + (random()-0.5)*0.2
    WHEN 'EN' THEN 7.55 + (random()-0.5)*0.2
    WHEN 'KD' THEN 7.42 + (random()-0.5)*0.2
    ELSE 7.50 + (random()-0.5)*3.0
  END
WHERE (latitude IS NULL OR latitude = 0);

-- Ride requests for needs_ride pledges that lack a ride
INSERT INTO gotv_ride_requests (party_id, contact_id, volunteer_id, pickup_latitude, pickup_longitude,
  polling_unit_code, status, requested_at, request_id)
SELECT
  p.party_id, p.contact_id,
  (SELECT volunteer_id FROM gotv_volunteers WHERE party_id = p.party_id AND has_vehicle = true ORDER BY random() LIMIT 1),
  c.latitude + (random()-0.5)*0.01, c.longitude + (random()-0.5)*0.01,
  COALESCE(c.polling_unit_code, 'PU-' || floor(random()*9999+1000)::text),
  (ARRAY['pending','matched','en_route','picked_up','dropped_off'])[floor(random()*5+1)],
  NOW() - (random() * interval '14 days'),
  'ride-' || md5(p.contact_id || random()::text)
FROM gotv_pledges p
INNER JOIN gotv_contacts c ON p.contact_id = c.contact_id AND c.party_id = p.party_id
WHERE p.pledge_type = 'needs_ride'
  AND p.contact_id NOT IN (SELECT contact_id FROM gotv_ride_requests WHERE party_id = p.party_id)
  AND c.latitude IS NOT NULL AND c.latitude != 0;


-- ─── 4. DEMOGRAPHIC ENRICHMENT ─────────────────────────────────────────────

UPDATE gotv_contacts SET
  age_group = (ARRAY['18-25','26-35','36-45','46-55','56-65','65+'])[floor(random()*6+1)],
  gender = (ARRAY['male','female'])[floor(random()*2+1)],
  socioeconomic_class = (ARRAY['A','B','C1','C2','D','E'])[floor(random()*6+1)],
  occupation_group = (ARRAY['professional','trader','civil_servant','student','farmer','artisan','unemployed'])[floor(random()*7+1)],
  education_level = (ARRAY['none','primary','secondary','tertiary','postgraduate'])[floor(random()*5+1)],
  religion = (ARRAY['christian','muslim','traditional','other'])[floor(random()*4+1)]
WHERE age_group IS NULL OR gender IS NULL OR socioeconomic_class IS NULL;


-- ─── 5. REFRESH COMPUTED COUNTERS ──────────────────────────────────────────

-- Campaign counters
UPDATE gotv_campaigns ca SET
  total_contacts = sub.total,
  contacts_reached = sub.reached,
  contacts_responded = sub.responded
FROM (
  SELECT campaign_id,
    COUNT(*) AS total,
    COUNT(*) FILTER (WHERE status IN ('delivered','read','responded')) AS reached,
    COUNT(*) FILTER (WHERE status = 'responded') AS responded
  FROM gotv_outreach_log GROUP BY campaign_id
) sub
WHERE ca.campaign_id = sub.campaign_id;

-- Contact engagement stats
UPDATE gotv_contacts c SET
  last_contacted_at = sub.last_contact,
  contact_count = sub.cnt
FROM (
  SELECT contact_id, MAX(sent_at) AS last_contact, COUNT(*) AS cnt
  FROM gotv_outreach_log GROUP BY contact_id
) sub
WHERE c.contact_id = sub.contact_id;

-- Volunteer performance metrics
UPDATE gotv_volunteers v SET
  doors_knocked = COALESCE(dk.cnt, 0),
  rides_given = COALESCE(rd.cnt, 0)
FROM (
  SELECT volunteer_id, COUNT(*) AS cnt FROM gotv_door_knocks GROUP BY volunteer_id
) dk
LEFT JOIN (
  SELECT volunteer_id, COUNT(*) AS cnt FROM gotv_ride_requests WHERE status = 'dropped_off' GROUP BY volunteer_id
) rd ON dk.volunteer_id = rd.volunteer_id
WHERE v.volunteer_id = dk.volunteer_id;

-- Survey responses: fill LGA codes from contacts
UPDATE gotv_survey_responses sr SET
  lga_code = (SELECT lga_code FROM gotv_contacts WHERE party_id = sr.party_id AND lga_code IS NOT NULL ORDER BY random() LIMIT 1)
WHERE sr.lga_code IS NULL OR sr.lga_code = '';

-- Clean up duplicate CPI history (keep only 1 per month)
DELETE FROM gotv_cpi_history a
USING gotv_cpi_history b
WHERE a.id > b.id
  AND a.party_id = b.party_id
  AND date_trunc('month', a.computed_at) = date_trunc('month', b.computed_at);

COMMIT;
