-- ============================================================================
-- GOTV Vetting Pipeline, Tasks & Location Assignment — Seed Data
-- ============================================================================
-- Populates vetting columns on existing volunteers, creates tasks, and
-- assigns locations. Safe to run repeatedly (idempotent).
--
-- Usage: psql -U ngapp -d ngapp -f scripts/seed_vetting_tasks.sql
-- ============================================================================

BEGIN;

-- ─── Add vetting columns if missing ────────────────────────────────────────

ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS vetting_status TEXT DEFAULT 'pending';
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS nin_verified BOOLEAN DEFAULT FALSE;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS nin_verified_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS training_completed BOOLEAN DEFAULT FALSE;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS training_completed_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS background_cleared BOOLEAN DEFAULT FALSE;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS approved_by TEXT DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS suspended_reason TEXT DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE gotv_volunteers ADD COLUMN IF NOT EXISTS assigned_polling_unit TEXT DEFAULT NULL;

-- ─── Create tasks table if missing ─────────────────────────────────────────

CREATE TABLE IF NOT EXISTS gotv_tasks (
    id SERIAL PRIMARY KEY,
    task_id TEXT UNIQUE NOT NULL,
    party_id INTEGER NOT NULL,
    task_type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    volunteer_id TEXT,
    ward_code TEXT,
    state_code TEXT,
    lga_code TEXT,
    target_count INTEGER DEFAULT 1,
    completed_count INTEGER DEFAULT 0,
    priority INTEGER DEFAULT 3,
    status TEXT DEFAULT 'unassigned',
    due_date DATE,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gotv_tasks_party ON gotv_tasks(party_id);
CREATE INDEX IF NOT EXISTS idx_gotv_tasks_volunteer ON gotv_tasks(volunteer_id);
CREATE INDEX IF NOT EXISTS idx_gotv_tasks_status ON gotv_tasks(party_id, status);

-- ─── Vetting Pipeline: Distribute volunteers across statuses ───────────────
-- 40% approved, 15% trained, 15% nin_verified, 20% pending, 5% suspended, 5% rejected

-- First reset all to pending so this is idempotent
UPDATE gotv_volunteers SET vetting_status = 'pending',
    nin_verified = FALSE, nin_verified_at = NULL,
    training_completed = FALSE, training_completed_at = NULL,
    background_cleared = FALSE, approved_by = NULL, approved_at = NULL,
    suspended_reason = NULL, suspended_at = NULL
WHERE vetting_status IS NULL OR vetting_status = 'pending';

-- Approved (40%): fully vetted, active volunteers
WITH ranked AS (
    SELECT volunteer_id, ROW_NUMBER() OVER (PARTITION BY party_id ORDER BY doors_knocked DESC, created_at ASC) as rn,
           COUNT(*) OVER (PARTITION BY party_id) as total
    FROM gotv_volunteers
)
UPDATE gotv_volunteers v
SET vetting_status = 'approved',
    nin_verified = TRUE,
    nin_verified_at = v.created_at + interval '1 day',
    training_completed = TRUE,
    training_completed_at = v.created_at + interval '3 days',
    background_cleared = TRUE,
    approved_by = 'coord-' || SUBSTRING(v.volunteer_id, 10, 4),
    approved_at = v.created_at + interval '5 days',
    is_active = TRUE
FROM ranked r
WHERE r.volunteer_id = v.volunteer_id AND r.rn <= (r.total * 0.40);

-- Trained (15%): NIN verified + training done, awaiting approval
WITH ranked AS (
    SELECT volunteer_id, ROW_NUMBER() OVER (PARTITION BY party_id ORDER BY created_at ASC) as rn,
           COUNT(*) OVER (PARTITION BY party_id) as total
    FROM gotv_volunteers WHERE vetting_status = 'pending'
)
UPDATE gotv_volunteers v
SET vetting_status = 'trained',
    nin_verified = TRUE,
    nin_verified_at = v.created_at + interval '2 days',
    training_completed = TRUE,
    training_completed_at = v.created_at + interval '4 days',
    is_active = FALSE
FROM ranked r
WHERE r.volunteer_id = v.volunteer_id AND r.rn <= (r.total * 0.37);

-- NIN Verified (15%): passed NIN check, awaiting training
WITH ranked AS (
    SELECT volunteer_id, ROW_NUMBER() OVER (PARTITION BY party_id ORDER BY created_at ASC) as rn,
           COUNT(*) OVER (PARTITION BY party_id) as total
    FROM gotv_volunteers WHERE vetting_status = 'pending'
)
UPDATE gotv_volunteers v
SET vetting_status = 'nin_verified',
    nin_verified = TRUE,
    nin_verified_at = v.created_at + interval '1 day',
    is_active = FALSE
FROM ranked r
WHERE r.volunteer_id = v.volunteer_id AND r.rn <= (r.total * 0.50);

-- Suspended (5%): previously approved, now suspended
WITH ranked AS (
    SELECT volunteer_id, ROW_NUMBER() OVER (PARTITION BY party_id ORDER BY RANDOM()) as rn,
           COUNT(*) OVER (PARTITION BY party_id) as total
    FROM gotv_volunteers WHERE vetting_status = 'pending'
)
UPDATE gotv_volunteers v
SET vetting_status = 'suspended',
    nin_verified = TRUE,
    nin_verified_at = v.created_at + interval '1 day',
    training_completed = TRUE,
    training_completed_at = v.created_at + interval '3 days',
    suspended_reason = (ARRAY[
        'Missed 3 consecutive shifts without notice',
        'Reported aggressive behavior toward voters',
        'Failed to report field activities for 2 weeks',
        'Under investigation for data irregularities',
        'Violated campaign communication guidelines'
    ])[FLOOR(RANDOM() * 5 + 1)],
    suspended_at = NOW() - (RANDOM() * interval '14 days'),
    is_active = FALSE
FROM ranked r
WHERE r.volunteer_id = v.volunteer_id AND r.rn <= (r.total * 0.25);

-- Rejected (5%): NIN check failed
WITH ranked AS (
    SELECT volunteer_id, ROW_NUMBER() OVER (PARTITION BY party_id ORDER BY RANDOM()) as rn,
           COUNT(*) OVER (PARTITION BY party_id) as total
    FROM gotv_volunteers WHERE vetting_status = 'pending'
)
UPDATE gotv_volunteers v
SET vetting_status = 'rejected',
    nin_verified = FALSE,
    suspended_reason = (ARRAY[
        'NIN verification returned mismatch',
        'Prior conviction for electoral fraud',
        'Identity documents expired',
        'Failed background check — undisclosed criminal record',
        'Provided falsified credentials'
    ])[FLOOR(RANDOM() * 5 + 1)],
    is_active = FALSE
FROM ranked r
WHERE r.volunteer_id = v.volunteer_id AND r.rn <= (r.total * 0.25);

-- Remaining stay as 'pending' (new registrations)

-- ─── Location Assignments ──────────────────────────────────────────────────
-- Assign approved/trained volunteers to Nigerian states/LGAs/wards

-- Lagos
UPDATE gotv_volunteers
SET assigned_state = 'Lagos', assigned_lga = 'Ikeja', assigned_ward = 'LA-IK-W01'
WHERE vetting_status = 'approved' AND assigned_state IS NULL
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status='approved' AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 15);

UPDATE gotv_volunteers
SET assigned_state = 'Lagos', assigned_lga = 'Surulere', assigned_ward = 'LA-SU-W03'
WHERE vetting_status = 'approved' AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status='approved' AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 12);

UPDATE gotv_volunteers
SET assigned_state = 'Lagos', assigned_lga = 'Alimosho', assigned_ward = 'LA-AL-W05'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 10);

-- Kano
UPDATE gotv_volunteers
SET assigned_state = 'Kano', assigned_lga = 'Nassarawa', assigned_ward = 'KN-NA-W02'
WHERE vetting_status = 'approved' AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status='approved' AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 12);

UPDATE gotv_volunteers
SET assigned_state = 'Kano', assigned_lga = 'Fagge', assigned_ward = 'KN-FA-W01'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 10);

-- Rivers
UPDATE gotv_volunteers
SET assigned_state = 'Rivers', assigned_lga = 'Port Harcourt', assigned_ward = 'RV-PH-W04'
WHERE vetting_status = 'approved' AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status='approved' AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 10);

-- FCT
UPDATE gotv_volunteers
SET assigned_state = 'FCT', assigned_lga = 'Abuja Municipal', assigned_ward = 'FC-AM-W01'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 10);

-- Oyo
UPDATE gotv_volunteers
SET assigned_state = 'Oyo', assigned_lga = 'Ibadan North', assigned_ward = 'OY-IN-W02'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 8);

-- Kaduna
UPDATE gotv_volunteers
SET assigned_state = 'Kaduna', assigned_lga = 'Kaduna North', assigned_ward = 'KD-KN-W03'
WHERE vetting_status = 'approved' AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status='approved' AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 8);

-- Anambra
UPDATE gotv_volunteers
SET assigned_state = 'Anambra', assigned_lga = 'Onitsha North', assigned_ward = 'AN-ON-W01'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 6);

-- Delta
UPDATE gotv_volunteers
SET assigned_state = 'Delta', assigned_lga = 'Warri South', assigned_ward = 'DT-WS-W02'
WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state = '')
  AND volunteer_id IN (SELECT volunteer_id FROM gotv_volunteers WHERE vetting_status IN ('approved','trained') AND (assigned_state IS NULL OR assigned_state='') ORDER BY RANDOM() LIMIT 6);

-- Assign remaining approved volunteers to various states
UPDATE gotv_volunteers
SET assigned_state = (ARRAY['Enugu','Imo','Edo','Plateau','Borno','Sokoto','Kwara','Osun','Ogun','Bauchi'])[FLOOR(RANDOM()*10+1)],
    assigned_lga = 'Central'
WHERE vetting_status = 'approved' AND (assigned_state IS NULL OR assigned_state = '');


-- ─── Tasks ─────────────────────────────────────────────────────────────────
-- Create realistic tasks across different types and statuses

DELETE FROM gotv_tasks WHERE task_id LIKE 'task-seed-%';

-- Door knock tasks (canvassing wards)
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, ward_code, state_code, lga_code, target_count, completed_count, priority, status, due_date, started_at, completed_at, created_at)
SELECT
    'task-seed-dk-' || i,
    1,
    'door_knock',
    'Canvass ' || (ARRAY['Ikeja Ward 1','Surulere Ward 3','Alimosho Ward 5','Nassarawa Ward 2','Port Harcourt Ward 4','Ibadan North Ward 2','Abuja Municipal Ward 1','Kaduna North Ward 3'])[((i-1) % 8) + 1],
    'Door-to-door canvassing — introduce candidate, collect pledges, note voter concerns',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' AND role='canvasser' ORDER BY RANDOM() LIMIT 1),
    (ARRAY['LA-IK-W01','LA-SU-W03','LA-AL-W05','KN-NA-W02','RV-PH-W04','OY-IN-W02','FC-AM-W01','KD-KN-W03'])[((i-1) % 8) + 1],
    (ARRAY['Lagos','Lagos','Lagos','Kano','Rivers','Oyo','FCT','Kaduna'])[((i-1) % 8) + 1],
    (ARRAY['Ikeja','Surulere','Alimosho','Nassarawa','Port Harcourt','Ibadan North','Abuja Municipal','Kaduna North'])[((i-1) % 8) + 1],
    50 + (i * 10),
    CASE WHEN i <= 5 THEN 50 + (i * 10) WHEN i <= 10 THEN (i * 8) ELSE 0 END,
    CASE WHEN i <= 3 THEN 5 WHEN i <= 8 THEN 4 WHEN i <= 15 THEN 3 ELSE 2 END,
    CASE WHEN i <= 5 THEN 'completed' WHEN i <= 10 THEN 'in_progress' WHEN i <= 15 THEN 'assigned' ELSE 'unassigned' END,
    CURRENT_DATE + (i * 2),
    CASE WHEN i <= 10 THEN NOW() - interval '3 days' + (i * interval '4 hours') ELSE NULL END,
    CASE WHEN i <= 5 THEN NOW() - interval '1 day' + (i * interval '2 hours') ELSE NULL END,
    NOW() - interval '7 days' + (i * interval '6 hours')
FROM generate_series(1, 20) i
ON CONFLICT (task_id) DO NOTHING;

-- Phone call tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, state_code, target_count, completed_count, priority, status, due_date, created_at)
SELECT
    'task-seed-pc-' || i,
    1,
    'phone_call',
    'Phone bank — ' || (ARRAY['Lagos','Kano','Rivers','FCT','Oyo','Kaduna','Anambra','Delta'])[((i-1) % 8) + 1] || ' contacts',
    'Call registered contacts to remind about election day, confirm pledge status, offer ride to polls',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' AND role IN ('caller','phone_banker') ORDER BY RANDOM() LIMIT 1),
    (ARRAY['Lagos','Kano','Rivers','FCT','Oyo','Kaduna','Anambra','Delta'])[((i-1) % 8) + 1],
    100 + (i * 20),
    CASE WHEN i <= 3 THEN 100 + (i * 20) WHEN i <= 6 THEN (i * 15) ELSE 0 END,
    CASE WHEN i <= 2 THEN 5 WHEN i <= 5 THEN 4 ELSE 3 END,
    CASE WHEN i <= 3 THEN 'completed' WHEN i <= 6 THEN 'in_progress' WHEN i <= 9 THEN 'assigned' ELSE 'unassigned' END,
    CURRENT_DATE + (i * 3),
    NOW() - interval '5 days' + (i * interval '8 hours')
FROM generate_series(1, 12) i
ON CONFLICT (task_id) DO NOTHING;

-- Ride duty tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, state_code, lga_code, target_count, priority, status, due_date, created_at)
SELECT
    'task-seed-rd-' || i,
    1,
    'ride_duty',
    'Driver standby — ' || (ARRAY['Ikeja','Surulere','Nassarawa','Port Harcourt','Abuja Municipal','Ibadan North'])[((i-1) % 6) + 1],
    'Available as ride-to-polls driver for election day. Cover assigned LGA polling units.',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' AND role='driver' ORDER BY RANDOM() LIMIT 1),
    (ARRAY['Lagos','Lagos','Kano','Rivers','FCT','Oyo'])[((i-1) % 6) + 1],
    (ARRAY['Ikeja','Surulere','Nassarawa','Port Harcourt','Abuja Municipal','Ibadan North'])[((i-1) % 6) + 1],
    8,
    CASE WHEN i <= 2 THEN 4 ELSE 3 END,
    CASE WHEN i <= 3 THEN 'assigned' ELSE 'unassigned' END,
    CURRENT_DATE + 7,
    NOW() - interval '3 days' + (i * interval '5 hours')
FROM generate_series(1, 8) i
ON CONFLICT (task_id) DO NOTHING;

-- Event setup tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, state_code, target_count, priority, status, due_date, created_at)
SELECT
    'task-seed-ev-' || i,
    1,
    'event_setup',
    (ARRAY['Set up rally at Tafawa Balewa Square','Town hall meeting — Alausa','Campaign materials at Ikeja City Mall','Rally setup — Wuse Market','Town hall — Port Harcourt Civic Centre'])[i],
    'Set up stage, sound system, banners, distribute flyers, coordinate volunteers on-site',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' AND role IN ('coordinator','team_lead') ORDER BY RANDOM() LIMIT 1),
    (ARRAY['Lagos','Lagos','Lagos','FCT','Rivers'])[i],
    1,
    5,
    CASE WHEN i <= 2 THEN 'completed' WHEN i <= 4 THEN 'assigned' ELSE 'unassigned' END,
    CURRENT_DATE + (i * 5),
    NOW() - interval '10 days' + (i * interval '2 days')
FROM generate_series(1, 5) i
ON CONFLICT (task_id) DO NOTHING;

-- Data collection / survey tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, ward_code, state_code, target_count, completed_count, priority, status, due_date, created_at)
SELECT
    'task-seed-dc-' || i,
    1,
    'data_collection',
    'Field survey — ' || (ARRAY['Lagos Island','Kano Municipal','Onitsha','Warri','Ibadan'])[i] || ' ward ' || i,
    'Collect voter sentiment survey (KOH indicators), record responses on mobile app',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' ORDER BY RANDOM() LIMIT 1),
    (ARRAY['LA-LI-W01','KN-MN-W02','AN-ON-W01','DT-WS-W02','OY-IB-W03'])[i],
    (ARRAY['Lagos','Kano','Anambra','Delta','Oyo'])[i],
    200,
    CASE WHEN i <= 2 THEN 200 WHEN i <= 3 THEN 120 ELSE 0 END,
    4,
    CASE WHEN i <= 2 THEN 'completed' WHEN i <= 3 THEN 'in_progress' WHEN i <= 4 THEN 'assigned' ELSE 'unassigned' END,
    CURRENT_DATE + (i * 4),
    NOW() - interval '8 days' + (i * interval '1 day')
FROM generate_series(1, 5) i
ON CONFLICT (task_id) DO NOTHING;

-- Materials distribution tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, state_code, target_count, priority, status, due_date, created_at)
SELECT
    'task-seed-md-' || i,
    1,
    'materials_distribution',
    'Distribute flyers — ' || (ARRAY['Lagos markets','Kano neighborhoods','Rivers campus areas','FCT government area','Oyo motor parks'])[i],
    'Distribute campaign flyers, posters, and branded materials in high-traffic areas',
    (ARRAY['Lagos','Kano','Rivers','FCT','Oyo'])[i],
    500 + (i * 100),
    3,
    'unassigned',
    CURRENT_DATE + (i * 2),
    NOW() - interval '2 days' + (i * interval '3 hours')
FROM generate_series(1, 5) i
ON CONFLICT (task_id) DO NOTHING;

-- Monitoring / observer tasks
INSERT INTO gotv_tasks (task_id, party_id, task_type, title, description, volunteer_id, state_code, target_count, priority, status, due_date, created_at)
SELECT
    'task-seed-mo-' || i,
    1,
    'monitoring',
    'Poll monitoring — ' || (ARRAY['Lagos Ikeja PU','Kano Nassarawa PU','Rivers PH PU','FCT Wuse PU','Oyo Ibadan PU','Kaduna Central PU'])[i],
    'Observe and report polling unit activities on election day. Document irregularities.',
    (SELECT volunteer_id FROM gotv_volunteers WHERE party_id=1 AND vetting_status='approved' AND role='observer' ORDER BY RANDOM() LIMIT 1),
    (ARRAY['Lagos','Kano','Rivers','FCT','Oyo','Kaduna'])[i],
    1,
    5,
    'assigned',
    CURRENT_DATE + 14,
    NOW() - interval '1 day'
FROM generate_series(1, 6) i
ON CONFLICT (task_id) DO NOTHING;


-- ─── Summary ───────────────────────────────────────────────────────────────

DO $$
DECLARE
    v_approved INT; v_trained INT; v_ninver INT; v_pending INT; v_suspended INT; v_rejected INT;
    t_total INT; t_assigned INT; t_progress INT; t_completed INT; t_unassigned INT;
    l_assigned INT;
BEGIN
    SELECT COUNT(*) FILTER (WHERE vetting_status='approved'),
           COUNT(*) FILTER (WHERE vetting_status='trained'),
           COUNT(*) FILTER (WHERE vetting_status='nin_verified'),
           COUNT(*) FILTER (WHERE vetting_status='pending'),
           COUNT(*) FILTER (WHERE vetting_status='suspended'),
           COUNT(*) FILTER (WHERE vetting_status='rejected')
    INTO v_approved, v_trained, v_ninver, v_pending, v_suspended, v_rejected
    FROM gotv_volunteers WHERE party_id = 1;

    SELECT COUNT(*),
           COUNT(*) FILTER (WHERE status='assigned'),
           COUNT(*) FILTER (WHERE status='in_progress'),
           COUNT(*) FILTER (WHERE status='completed'),
           COUNT(*) FILTER (WHERE status='unassigned')
    INTO t_total, t_assigned, t_progress, t_completed, t_unassigned
    FROM gotv_tasks WHERE party_id = 1;

    SELECT COUNT(*) INTO l_assigned
    FROM gotv_volunteers WHERE party_id = 1 AND assigned_state IS NOT NULL AND assigned_state != '';

    RAISE NOTICE '═══════════════════════════════════════════════════════════';
    RAISE NOTICE 'GOTV Vetting Pipeline (party_id=1):';
    RAISE NOTICE '  Approved: %, Trained: %, NIN Verified: %', v_approved, v_trained, v_ninver;
    RAISE NOTICE '  Pending: %, Suspended: %, Rejected: %', v_pending, v_suspended, v_rejected;
    RAISE NOTICE '';
    RAISE NOTICE 'Tasks (party_id=1): % total', t_total;
    RAISE NOTICE '  Completed: %, In Progress: %, Assigned: %, Unassigned: %', t_completed, t_progress, t_assigned, t_unassigned;
    RAISE NOTICE '';
    RAISE NOTICE 'Location Assignments: % volunteers assigned to locations', l_assigned;
    RAISE NOTICE '═══════════════════════════════════════════════════════════';
END $$;

COMMIT;
