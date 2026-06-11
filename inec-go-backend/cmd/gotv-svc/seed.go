package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/rs/zerolog/log"
)

func seedGOTVData(db *sql.DB) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM gotv_campaigns").Scan(&count)
	if err != nil || count > 0 {
		return // tables don't exist yet or already seeded
	}

	// Ensure at least one party exists for FK references
	db.Exec(`INSERT INTO parties (id, code, name, abbreviation, color) VALUES (1, 'APC', 'All Progressives Congress', 'APC', '#009639') ON CONFLICT DO NOTHING`)
	db.Exec(`INSERT INTO parties (id, code, name, abbreviation, color) VALUES (2, 'PDP', 'Peoples Democratic Party', 'PDP', '#E30A0A') ON CONFLICT DO NOTHING`)
	db.Exec(`INSERT INTO parties (id, code, name, abbreviation, color) VALUES (3, 'LP', 'Labour Party', 'LP', '#4C8C2B') ON CONFLICT DO NOTHING`)

	log.Info().Msg("Seeding GOTV demo data for party_id=1...")

	now := time.Now().UTC()
	pid := 1

	// ─── Campaigns ──────────────────────────────────────────────────────
	campaigns := []struct {
		id, name, ctype, state, status string
		contacts, reached              int
	}{
		{randID(), "Lagos State SMS Outreach", "sms", "Lagos", "active", 15000, 8742},
		{randID(), "Kano Door-to-Door Campaign", "door_to_door", "Kano", "active", 22000, 3200},
		{randID(), "FCT WhatsApp Reminders", "whatsapp", "FCT", "scheduled", 8500, 0},
		{randID(), "Rivers State Push Notifications", "push", "Rivers", "draft", 12000, 0},
		{randID(), "Oyo USSD Get-Out-The-Vote", "ussd", "Oyo", "completed", 18000, 17500},
		{randID(), "National Election Day Reminder", "sms", "", "active", 45000, 12000},
		{randID(), "Delta State Youth Mobilization", "whatsapp", "Delta", "active", 9000, 4100},
		{randID(), "Kaduna Women Voter Drive", "sms", "Kaduna", "paused", 11000, 6300},
	}
	for _, c := range campaigns {
		state := sql.NullString{String: c.state, Valid: c.state != ""}
		db.Exec(`INSERT INTO gotv_campaigns (campaign_id, party_id, name, campaign_type, target_state, status, total_contacts, contacts_reached, created_by, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'admin',$9) ON CONFLICT DO NOTHING`,
			c.id, pid, c.name, c.ctype, state, c.status, c.contacts, c.reached, now)
	}

	// ─── Contacts ───────────────────────────────────────────────────────
	type contact struct {
		id, name, phone, state, lga, status string
		tags                                string
	}
	contacts := []contact{
		{randID(), "Adebayo Ogundimu", "08012345001", "Lagos", "Ikeja", "pledged", "{supporter,youth}"},
		{randID(), "Fatima Abdullahi", "08012345002", "Kano", "Nassarawa", "confirmed", "{women,core}"},
		{randID(), "Chinedu Okafor", "08012345003", "Anambra", "Onitsha North", "unknown", "{new}"},
		{randID(), "Blessing Eze", "08012345004", "Enugu", "Enugu North", "pledged", "{volunteer}"},
		{randID(), "Musa Ibrahim", "08012345005", "Kaduna", "Chikun", "unreachable", "{}"},
		{randID(), "Amina Yusuf", "08012345006", "FCT", "AMAC", "confirmed", "{core,leader}"},
		{randID(), "Oluwatobi Adeleke", "08012345007", "Oyo", "Ibadan North", "pledged", "{youth}"},
		{randID(), "Ngozi Nwankwo", "08012345008", "Rivers", "Port Harcourt", "declined", "{}"},
		{randID(), "Ifeanyi Uba", "08012345009", "Delta", "Oshimili South", "pledged", "{business}"},
		{randID(), "Hauwa Bello", "08012345010", "Borno", "Maiduguri", "confirmed", "{women}"},
		{randID(), "Tunde Bakare", "08012345011", "Lagos", "Surulere", "pledged", "{faith,leader}"},
		{randID(), "Zainab Mohammed", "08012345012", "Kano", "Kano Municipal", "confirmed", "{core}"},
		{randID(), "Emeka Nwosu", "08012345013", "Imo", "Owerri Municipal", "unknown", "{new}"},
		{randID(), "Folake Adekunle", "08012345014", "Ogun", "Abeokuta South", "pledged", "{youth,women}"},
		{randID(), "Yusuf Garba", "08012345015", "Plateau", "Jos North", "confirmed", "{volunteer}"},
	}
	for _, c := range contacts {
		consentID := "consent-" + randID()
		db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, voter_status, tags, opted_out, consent_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,FALSE,$10,$11) ON CONFLICT DO NOTHING`,
			c.id, pid, c.phone, c.phone, c.name, c.state, c.lga, c.status, c.tags, consentID, now)
	}

	// ─── Volunteers ─────────────────────────────────────────────────────
	type vol struct {
		id, name, phone, role, state, lga string
		vehicle                           bool
		cap                               int
		lat, lng                          float64
		doors, calls, rides               int
	}
	volunteers := []vol{
		{randID(), "Abubakar Suleiman", "08098765001", "canvasser", "Lagos", "Ikeja", false, 0, 6.601, 3.351, 42, 15, 0},
		{randID(), "Chioma Obi", "08098765002", "driver", "Lagos", "Victoria Island", true, 4, 6.428, 3.422, 0, 0, 18},
		{randID(), "Olumide Fajemisin", "08098765003", "coordinator", "Oyo", "Ibadan North", true, 6, 7.396, 3.917, 120, 85, 5},
		{randID(), "Halima Baba", "08098765004", "canvasser", "Kano", "Nassarawa", false, 0, 12.002, 8.519, 88, 45, 0},
		{randID(), "Emeka Uzor", "08098765005", "driver", "Rivers", "Port Harcourt", true, 3, 4.815, 7.033, 0, 0, 24},
		{randID(), "Funmilayo Oni", "08098765006", "caller", "FCT", "AMAC", false, 0, 9.058, 7.491, 0, 210, 0},
		{randID(), "Ibrahim Danjuma", "08098765007", "canvasser", "Kaduna", "Chikun", false, 0, 10.526, 7.439, 56, 20, 0},
		{randID(), "Ngozi Ikemba", "08098765008", "observer", "Anambra", "Onitsha North", false, 0, 6.145, 6.783, 0, 0, 0},
		{randID(), "Yakubu Aliyu", "08098765009", "driver", "Kano", "Kano Municipal", true, 5, 12.000, 8.517, 0, 0, 31},
		{randID(), "Adaeze Nnamdi", "08098765010", "canvasser", "Enugu", "Enugu North", false, 0, 6.441, 7.499, 67, 30, 0},
	}
	for _, v := range volunteers {
		db.Exec(`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, assigned_state, assigned_lga, has_vehicle, vehicle_capacity, latitude, longitude, doors_knocked, calls_made, rides_given, is_active, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,TRUE,$15) ON CONFLICT DO NOTHING`,
			v.id, pid, v.name, v.phone, v.role, v.state, v.lga, v.vehicle, v.cap, v.lat, v.lng, v.doors, v.calls, v.rides, now)
	}

	// ─── Pledges ────────────────────────────────────────────────────────
	pledgeStatuses := []string{"pledged", "pledged", "reminded", "reminded", "confirmed_day_of", "confirmed_day_of", "fulfilled", "fulfilled", "fulfilled", "broken"}
	for i, c := range contacts[:10] {
		ptype := "will_vote"
		if i%3 == 0 {
			ptype = "needs_ride"
		}
		db.Exec(`INSERT INTO gotv_pledges (pledge_id, party_id, contact_id, pledge_type, status, created_at)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			randID(), pid, c.id, ptype, pledgeStatuses[i], now.Add(-time.Duration(10-i)*24*time.Hour))
	}

	// ─── Ride Requests ──────────────────────────────────────────────────
	rideStatuses := []string{"pending", "pending", "matched", "en_route", "picked_up", "dropped_off", "cancelled"}
	puCodes := []string{"LA/IKJ/001", "LA/IKJ/002", "KN/NAS/003", "OY/IBN/001", "FC/AMC/001", "RI/PH/002", "KD/CHK/001"}
	pickupLats := []float64{6.610, 6.430, 12.010, 7.400, 9.060, 4.820, 10.530}
	pickupLngs := []float64{3.360, 3.430, 8.525, 3.920, 7.495, 7.040, 7.445}
	for i, s := range rideStatuses {
		ci := i % len(contacts)
		vi := i % len(volunteers)
		var volID sql.NullString
		if s != "pending" {
			volID = sql.NullString{String: volunteers[vi].id, Valid: true}
		}
		db.Exec(`INSERT INTO gotv_ride_requests (request_id, party_id, contact_id, volunteer_id, pickup_latitude, pickup_longitude, polling_unit_code, status, distance_km, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			randID(), 1, contacts[ci].id, volID,
			pickupLats[i], pickupLngs[i],
			puCodes[i], s, float64(i+1)*1.5, now.Add(-time.Duration(7-i)*time.Hour))
	}

	// ─── V2: Segments ──────────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_segments (segment_id, party_id, name, filters, created_at) VALUES
		($1, $2, 'Lagos Youth Voters', '[{"field":"state_code","operator":"eq","value":"Lagos"},{"field":"voter_status","operator":"neq","value":"declined"}]', $3),
		($4, $5, 'Pledged Supporters', '[{"field":"voter_status","operator":"eq","value":"pledged"}]', $6),
		($7, $8, 'Unreached Contacts', '[{"field":"voter_status","operator":"eq","value":"unknown"}]', $9)
		ON CONFLICT DO NOTHING`,
		"seg-"+randID()[:8], pid, now, "seg-"+randID()[:8], pid, now, "seg-"+randID()[:8], pid, now)

	// ─── V2: AI Variants ───────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_ai_variants (variant_id, party_id, base_message, variant_text, target_state, channel, variant_index, created_at) VALUES
		($1, $2, 'Remember to vote on election day!', 'Your vote is your voice! Make it count this Saturday at your polling unit.', 'Lagos', 'sms', 0, $3),
		($4, $5, 'Remember to vote on election day!', 'Omo Lagos, na your future dey your hand! Go vote this Saturday!', 'Lagos', 'whatsapp', 1, $6),
		($7, $8, 'Every vote matters in this election', 'Ka ku zo ku kadda kuri''a. Kuri''ar ku na da muhimmanci!', 'Kano', 'sms', 0, $9),
		($10, $11, 'Get a free ride to your polling unit', 'Need a ride? Reply RIDE and we will send a driver to pick you up!', '', 'sms', 0, $12)
		ON CONFLICT DO NOTHING`,
		"var-"+randID()[:8], pid, now, "var-"+randID()[:8], pid, now,
		"var-"+randID()[:8], pid, now, "var-"+randID()[:8], pid, now)

	// ─── V2: Sequences ─────────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_campaign_sequences (sequence_id, party_id, name, waves, status, created_at) VALUES
		($1, $2, 'Lagos 3-Wave Outreach', '[{"wave_number":1,"channel":"sms","delay_hours":0,"template":"Initial reminder"},{"wave_number":2,"channel":"whatsapp","delay_hours":48,"template":"Follow-up with buttons"},{"wave_number":3,"channel":"phone","delay_hours":96,"template":"Final personal call"}]', 'active', $3)
		ON CONFLICT DO NOTHING`,
		"seq-"+randID()[:8], pid, now)

	// ─── V2: Challenges (Gamification) ─────────────────────────────────
	db.Exec(`INSERT INTO gotv_challenges (challenge_id, party_id, name, target_metric, target_value, reward_description, starts_at, ends_at) VALUES
		($1, $2, 'Door-to-Door Champion', 'doors_knocked', 50, 'Custom party jersey + N5,000 airtime', $3, $4),
		($5, $6, 'Call Center Star', 'calls_made', 100, 'N10,000 bonus + recognition at rally', $7, $8)
		ON CONFLICT DO NOTHING`,
		"chal-"+randID()[:8], pid, now, now.Add(7*24*time.Hour),
		"chal-"+randID()[:8], pid, now, now.Add(7*24*time.Hour))

	// ─── V2: Outreach Log (for ROI analytics) ──────────────────────────
	channels := []string{"sms", "sms", "whatsapp", "whatsapp", "push", "email", "sms", "whatsapp"}
	statuses := []string{"delivered", "delivered", "delivered", "failed", "delivered", "delivered", "pending", "delivered"}
	costs := []int{400, 400, 250, 250, 50, 300, 400, 250}
	for i, ch := range channels {
		db.Exec(`INSERT INTO gotv_outreach_log (party_id, campaign_id, contact_id, channel, status, cost_kobo, sent_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			pid, campaigns[i%len(campaigns)].id, contacts[i%len(contacts)].id,
			ch, statuses[i], costs[i], now.Add(-time.Duration(i)*time.Hour))
	}

	// ─── V2: Territories ──────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_territories (territory_id, party_id, volunteer_id, ward_code, contact_count, status) VALUES
		($1, $2, $3, 'LA/IKJ/W01', 45, 'active'),
		($4, $5, $6, 'LA/IKJ/W02', 32, 'active'),
		($7, $8, $9, 'KN/NAS/W01', 67, 'active'),
		($10, $11, $12, 'OY/IBN/W01', 28, 'pending'),
		($13, $14, $15, 'FC/AMC/W01', 51, 'active')
		ON CONFLICT DO NOTHING`,
		"terr-"+randID()[:8], pid, volunteers[0].id,
		"terr-"+randID()[:8], pid, volunteers[1].id,
		"terr-"+randID()[:8], pid, volunteers[3].id,
		"terr-"+randID()[:8], pid, volunteers[2].id,
		"terr-"+randID()[:8], pid, volunteers[5].id)

	// ─── V2: Field Reports ─────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_field_reports (report_id, party_id, issue_type, description, ward_code, latitude, longitude, source, resolved) VALUES
		($1, $2, 'voter_intimidation', 'Suspected thugs blocking entrance to polling unit on Adeniyi Jones Ave', 'LA/IKJ/W01', 6.601, 3.351, 'mobile', FALSE),
		($3, $4, 'equipment_failure', 'BVAS machine not working at this unit since 9am, voters leaving', 'KN/NAS/W01', 12.002, 8.519, 'mobile', FALSE),
		($5, $6, 'access_blocked', 'Road to polling unit flooded after overnight rain, voters cannot access', 'RI/PH/W01', 4.815, 7.033, 'web', TRUE),
		($7, $8, 'ballot_irregularity', 'Presiding officer seen thumbprinting ballots before opening', 'KD/CHK/W01', 10.526, 7.439, 'mobile', FALSE)
		ON CONFLICT DO NOTHING`,
		"report-"+randID()[:8], pid, "report-"+randID()[:8], pid,
		"report-"+randID()[:8], pid, "report-"+randID()[:8], pid)

	// ─── V2: Voice Calls ───────────────────────────────────────────────
	db.Exec(`INSERT INTO gotv_voice_calls (call_id, party_id, campaign_id, contact_id, phone_number, status, created_at) VALUES
		($1, $2, $3, $4, '08012345001', 'completed', $5),
		($6, $7, $8, $9, '08012345002', 'in_progress', $10),
		($11, $12, $13, $14, '08012345003', 'failed', $15)
		ON CONFLICT DO NOTHING`,
		"vc-"+randID()[:8], pid, campaigns[0].id, contacts[0].id, now.Add(-2*time.Hour),
		"vc-"+randID()[:8], pid, campaigns[0].id, contacts[1].id, now.Add(-30*time.Minute),
		"vc-"+randID()[:8], pid, campaigns[0].id, contacts[2].id, now.Add(-1*time.Hour))

	log.Info().Msg("GOTV demo data seeded successfully (V1 + V2)")
}

func randID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
