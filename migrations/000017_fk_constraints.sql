-- Migration 000017: Foreign Key Constraints on Legacy GOTV Tables
-- Adds referential integrity to prevent orphan records.
-- Uses IF NOT EXISTS pattern via DO blocks for idempotency.

BEGIN;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_pledges → gotv_contacts
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_pledge_contact'
  ) THEN
    -- Clean up orphan pledges first
    UPDATE gotv_pledges SET contact_id = NULL
    WHERE contact_id IS NOT NULL
      AND contact_id NOT IN (SELECT contact_id FROM gotv_contacts);

    ALTER TABLE gotv_pledges
      ADD CONSTRAINT fk_pledge_contact
      FOREIGN KEY (contact_id) REFERENCES gotv_contacts(contact_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_ride_requests → gotv_contacts (voter requesting ride)
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_ride_contact'
  ) THEN
    UPDATE gotv_ride_requests SET contact_id = NULL
    WHERE contact_id IS NOT NULL
      AND contact_id NOT IN (SELECT contact_id FROM gotv_contacts);

    ALTER TABLE gotv_ride_requests
      ADD CONSTRAINT fk_ride_contact
      FOREIGN KEY (contact_id) REFERENCES gotv_contacts(contact_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_ride_requests → gotv_volunteers (assigned driver)
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_ride_volunteer'
  ) THEN
    UPDATE gotv_ride_requests SET volunteer_id = NULL
    WHERE volunteer_id IS NOT NULL
      AND volunteer_id NOT IN (SELECT volunteer_id FROM gotv_volunteers);

    ALTER TABLE gotv_ride_requests
      ADD CONSTRAINT fk_ride_volunteer
      FOREIGN KEY (volunteer_id) REFERENCES gotv_volunteers(volunteer_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_door_knocks → gotv_volunteers
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_knock_volunteer'
  ) THEN
    UPDATE gotv_door_knocks SET volunteer_id = NULL
    WHERE volunteer_id IS NOT NULL
      AND volunteer_id NOT IN (SELECT volunteer_id FROM gotv_volunteers);

    ALTER TABLE gotv_door_knocks
      ADD CONSTRAINT fk_knock_volunteer
      FOREIGN KEY (volunteer_id) REFERENCES gotv_volunteers(volunteer_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_door_knocks → gotv_contacts
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_knock_contact'
  ) THEN
    UPDATE gotv_door_knocks SET contact_id = NULL
    WHERE contact_id IS NOT NULL
      AND contact_id NOT IN (SELECT contact_id FROM gotv_contacts);

    ALTER TABLE gotv_door_knocks
      ADD CONSTRAINT fk_knock_contact
      FOREIGN KEY (contact_id) REFERENCES gotv_contacts(contact_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_tasks → gotv_volunteers
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_task_volunteer'
  ) THEN
    UPDATE gotv_tasks SET assigned_volunteer_id = NULL
    WHERE assigned_volunteer_id IS NOT NULL
      AND assigned_volunteer_id NOT IN (SELECT volunteer_id FROM gotv_volunteers);

    ALTER TABLE gotv_tasks
      ADD CONSTRAINT fk_task_volunteer
      FOREIGN KEY (assigned_volunteer_id) REFERENCES gotv_volunteers(volunteer_id)
      ON DELETE SET NULL;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_outreach_log → gotv_campaigns
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_outreach_campaign'
  ) THEN
    DELETE FROM gotv_outreach_log
    WHERE campaign_id IS NOT NULL
      AND campaign_id NOT IN (SELECT campaign_id FROM gotv_campaigns);

    ALTER TABLE gotv_outreach_log
      ADD CONSTRAINT fk_outreach_campaign
      FOREIGN KEY (campaign_id) REFERENCES gotv_campaigns(campaign_id)
      ON DELETE CASCADE;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_contact_scores → gotv_contacts
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_score_contact'
  ) THEN
    ALTER TABLE gotv_contact_scores
      ADD CONSTRAINT fk_score_contact
      FOREIGN KEY (contact_id) REFERENCES gotv_contacts(contact_id)
      ON DELETE CASCADE;
  END IF;
END $$;

-- ═══════════════════════════════════════════════════════════════════════════
-- gotv_volunteer_badges → gotv_volunteers
-- ═══════════════════════════════════════════════════════════════════════════

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_badge_volunteer'
  ) THEN
    ALTER TABLE gotv_volunteer_badges
      ADD CONSTRAINT fk_badge_volunteer
      FOREIGN KEY (volunteer_id) REFERENCES gotv_volunteers(volunteer_id)
      ON DELETE CASCADE;
  END IF;
END $$;

COMMIT;
