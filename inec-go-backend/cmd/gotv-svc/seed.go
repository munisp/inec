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
		db.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, full_name_encrypted, state_code, lga_code, voter_status, tags, opted_out, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,FALSE,$10) ON CONFLICT DO NOTHING`,
			c.id, pid, c.phone, c.phone, c.name, c.state, c.lga, c.status, c.tags, now)
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

	log.Info().Msg("GOTV demo data seeded successfully")
}

func randID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
