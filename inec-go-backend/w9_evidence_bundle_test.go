package main

import (
	"context"
	"testing"
)

// W2→W4 handoff: the evidence bundle must bind the canonical result set —
// finalized-only, rerun-child overrides parent for scoped PUs.
func TestEvidenceBundleUsesCanonicalResults(t *testing.T) {
	setupLifecycleTestDB(t) // full production schema via runMigrations
	if db == nil {
		t.Skip("PG unavailable")
	}

	seed := `
	INSERT INTO parties (code, name, abbreviation) VALUES ('APC','All Progressives','APC'),('PDP','Peoples Democratic','PDP') ON CONFLICT (code) DO NOTHING;
	INSERT INTO states (code, name, geo_zone) VALUES ('S9','State9','NC') ON CONFLICT (code) DO NOTHING;
	INSERT INTO lgas (code, name, state_code) VALUES ('L9','LGA9','S9') ON CONFLICT (code) DO NOTHING;
	INSERT INTO wards (code, name, lga_code) VALUES ('W9','Ward9','L9') ON CONFLICT (code) DO NOTHING;
	INSERT INTO polling_units (code, name, ward_code, registered_voters) VALUES
		('PU-9A','PU 9A','W9',1000),('PU-9B','PU 9B','W9',1000),('PU-9C','PU 9C','W9',1000) ON CONFLICT (code) DO NOTHING;
	INSERT INTO elections (id, title, election_type, election_date, status) VALUES (9001,'Parent','presidential','2027-01-01','collating') ON CONFLICT DO NOTHING;
	INSERT INTO elections (id, title, election_type, election_date, status, parent_election_id, election_kind)
		VALUES (9002,'Rerun','presidential','2027-02-01','collating',9001,'rerun') ON CONFLICT DO NOTHING;
	INSERT INTO rerun_scopes (election_id, scope_type, area_code, reason) VALUES (9002,'polling_unit','PU-9B','violence') ON CONFLICT DO NOTHING;
	-- Parent: PU-9A finalized; PU-9B finalized but OVERRIDDEN by the rerun
	-- child's result; PU-9C merely 'validated' — must NOT enter the bundle.
	INSERT INTO results (id, election_id, polling_unit_code, status, total_valid_votes, rejected_votes, total_votes_cast, accredited_voters, ec8a_hash)
		VALUES
		(90011, 9001, 'PU-9A', 'finalized', 300, 5, 305, 400, 'hash-a'),
		(90012, 9001, 'PU-9B', 'finalized', 200, 2, 202, 400, 'hash-b-parent'),
		(90014, 9001, 'PU-9C', 'validated', 199, 2, 201, 400, 'hash-c-validated'),
		(90013, 9002, 'PU-9B', 'finalized', 210, 3, 213, 400, 'hash-b-rerun')
	ON CONFLICT (id) DO NOTHING;
	INSERT INTO result_party_scores (result_id, party_code, votes) VALUES
		(90011, 'APC', 300), (90013, 'APC', 210);`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	children, totals, err := collectCollationEvidence(context.Background(), tx, 9001, "state", "S9")
	if err != nil {
		t.Fatalf("collectCollationEvidence: %v", err)
	}

	got := map[int64]bool{}
	for _, c := range children {
		id, _ := c["result_id"].(int64)
		got[id] = true
	}
	if !got[90011] {
		t.Error("parent finalized result PU-9A missing from bundle")
	}
	if !got[90013] {
		t.Error("rerun child result for PU-9B missing from bundle (override)")
	}
	if got[90012] {
		t.Error("parent result for PU-9B must be OVERRIDDEN by the rerun child, not bundled")
	}
	if got[90014] {
		t.Error("merely-'validated' result must not enter the bundle (finalized-only)")
	}
	// Party totals must reflect the canonical set only: 300 + 210.
	if apc := toInt64(totals["APC"]); apc != 510 {
		t.Errorf("party totals = %d, want 510 (parent PU-9A + rerun PU-9B only)", apc)
	}
}
