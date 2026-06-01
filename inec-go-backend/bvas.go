package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

// ── BVAS Device Registry & Accreditation ──

type BVASDevice struct {
	ID              string  `json:"id"`
	SerialNumber    string  `json:"serial_number"`
	PollingUnitCode string  `json:"polling_unit_code"`
	ElectionID      int     `json:"election_id"`
	Status          string  `json:"status"`
	BatteryLevel    int     `json:"battery_level"`
	FirmwareVersion string  `json:"firmware_version"`
	LastSyncAt      string  `json:"last_sync_at"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	AssignedOfficer int     `json:"assigned_officer"`
}

type AccreditationEvent struct {
	ID              int64  `json:"id"`
	DeviceID        string `json:"device_id"`
	ElectionID      int    `json:"election_id"`
	PollingUnitCode string `json:"polling_unit_code"`
	VoterPVCNumber  string `json:"voter_pvc_number"`
	BiometricMatch  bool   `json:"biometric_match"`
	PVCVerified     bool   `json:"pvc_verified"`
	AccreditedAt    string `json:"accredited_at"`
	Method          string `json:"method"`
}

type AccreditationReconciliation struct {
	PollingUnitCode   string  `json:"polling_unit_code"`
	PUName            string  `json:"pu_name"`
	BVASAccredited    int     `json:"bvas_accredited"`
	ResultAccredited  int     `json:"result_accredited"`
	Discrepancy       int     `json:"discrepancy"`
	DiscrepancyPct    float64 `json:"discrepancy_pct"`
	FlagLevel         string  `json:"flag_level"`
	BiometricPassRate float64 `json:"biometric_pass_rate"`
	PVCVerifyRate     float64 `json:"pvc_verify_rate"`
}

var (
	bvasDevices    = make(map[string]*BVASDevice)
	bvasAccEvents  []AccreditationEvent
	bvasAccCounts  = make(map[string]int)
	bvasAccMu      sync.RWMutex
	bvasNextEventID int64 = 1
)

func initBVASTables(database *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS bvas_devices (
		id TEXT PRIMARY KEY,
		serial_number TEXT UNIQUE NOT NULL,
		polling_unit_code TEXT,
		election_id INTEGER,
		status TEXT NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','deployed','active','offline','faulty','decommissioned')),
		battery_level INTEGER DEFAULT 100,
		firmware_version TEXT DEFAULT '3.2.1',
		last_sync_at TIMESTAMP,
		latitude REAL,
		longitude REAL,
		assigned_officer INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (polling_unit_code) REFERENCES polling_units(code),
		FOREIGN KEY (election_id) REFERENCES elections(id)
	);
	CREATE TABLE IF NOT EXISTS bvas_accreditations (
		id SERIAL PRIMARY KEY,
		device_id TEXT NOT NULL,
		election_id INTEGER NOT NULL,
		polling_unit_code TEXT NOT NULL,
		voter_pvc_hash TEXT NOT NULL,
		biometric_match INTEGER NOT NULL DEFAULT 0,
		pvc_verified INTEGER NOT NULL DEFAULT 0,
		method TEXT NOT NULL DEFAULT 'biometric' CHECK(method IN ('biometric','manual','override')),
		accredited_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced_at TIMESTAMP,
		FOREIGN KEY (device_id) REFERENCES bvas_devices(id),
		FOREIGN KEY (election_id) REFERENCES elections(id),
		FOREIGN KEY (polling_unit_code) REFERENCES polling_units(code)
	);
	CREATE INDEX IF NOT EXISTS idx_bvas_acc_pu ON bvas_accreditations(polling_unit_code, election_id);
	CREATE INDEX IF NOT EXISTS idx_bvas_acc_device ON bvas_accreditations(device_id);
	CREATE INDEX IF NOT EXISTS idx_bvas_devices_pu ON bvas_devices(polling_unit_code);
	`
	execMulti(database, schema)
}

func seedBVASDevices(database *sql.DB) {
	var count int
	database.QueryRow("SELECT COUNT(*) FROM bvas_devices").Scan(&count)
	if count > 0 {
		return
	}

	type puInfo struct {
		code string
		lat  float64
		lon  float64
	}
	puRows, _ := database.Query("SELECT code, latitude, longitude FROM polling_units WHERE latitude IS NOT NULL LIMIT 500")
	var pus []puInfo
	for puRows.Next() {
		var code string
		var lat, lon sql.NullFloat64
		puRows.Scan(&code, &lat, &lon)
		latV, lonV := 0.0, 0.0
		if lat.Valid { latV = lat.Float64 }
		if lon.Valid { lonV = lon.Float64 }
		pus = append(pus, puInfo{code, latV, lonV})
	}
	puRows.Close()

	rng := rand.New(rand.NewSource(42))

	tx, _ := database.Begin()
	for i, pu := range pus {
		devID := fmt.Sprintf("BVAS-%05d", i+1)
		serial := fmt.Sprintf("INEC-BVAS-2027-%06d", i+1)
		battery := 60 + rng.Intn(41)
		statuses := []string{"active", "active", "active", "active", "deployed", "offline"}
		st := statuses[rng.Intn(len(statuses))]
		tx.Exec(`INSERT INTO bvas_devices (id, serial_number, polling_unit_code, election_id, status, battery_level, firmware_version, last_sync_at, latitude, longitude)
			VALUES (?,?,?,1,?,?,?,NOW() + CAST(? AS INTERVAL),?,?)`,
			devID, serial, pu.code, st, battery, "3.2.1",
			fmt.Sprintf("-%d minutes", rng.Intn(120)), pu.lat, pu.lon)
	}
	tx.Commit()

	seedBVASAccreditations(database, rng)
}

func seedBVASAccreditations(database *sql.DB, rng *rand.Rand) {
	devRows, _ := database.Query("SELECT id, polling_unit_code FROM bvas_devices WHERE status='active' AND polling_unit_code IS NOT NULL")
	type devPU struct{ devID, puCode string }
	var devices []devPU
	for devRows.Next() {
		var d devPU
		devRows.Scan(&d.devID, &d.puCode)
		devices = append(devices, d)
	}
	devRows.Close()

	tx, _ := database.Begin()
	for _, d := range devices {
		var regVoters int
		database.QueryRow("SELECT registered_voters FROM polling_units WHERE code=?", d.puCode).Scan(&regVoters)
		if regVoters == 0 {
			regVoters = 500
		}
		numAccredited := regVoters / 10
		if numAccredited > 50 {
			numAccredited = 50
		}

		for i := 0; i < numAccredited; i++ {
			pvcNum := fmt.Sprintf("PVC-%s-%06d", d.puCode, i+1)
			h := sha256.Sum256([]byte(pvcNum))
			pvcHash := hex.EncodeToString(h[:])
			bioMatch := rng.Float64() < 0.97
			pvcOK := rng.Float64() < 0.99
			methods := []string{"biometric", "biometric", "biometric", "biometric", "manual"}
			method := methods[rng.Intn(len(methods))]
			tx.Exec(`INSERT INTO bvas_accreditations (device_id, election_id, polling_unit_code, voter_pvc_hash, biometric_match, pvc_verified, method, accredited_at, synced_at)
				VALUES (?,1,?,?,?,?,?,NOW() + CAST(? AS INTERVAL),NOW() + CAST(? AS INTERVAL))`,
				d.devID, d.puCode, pvcHash,
				boolToInt(bioMatch), boolToInt(pvcOK), method,
				fmt.Sprintf("-%d minutes", rng.Intn(480)),
				fmt.Sprintf("-%d minutes", rng.Intn(60)))
		}
	}
	tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── BVAS API Handlers ──

func handleListBVASDevices(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	status := r.URL.Query().Get("status")
	puCode := r.URL.Query().Get("polling_unit_code")
	limit := queryParamInt(r, "limit", 50)
	offset := queryParamInt(r, "offset", 0)

	q := "SELECT d.*, pu.name as pu_name FROM bvas_devices d LEFT JOIN polling_units pu ON pu.code=d.polling_unit_code WHERE d.election_id=?"
	params := []interface{}{eid}
	if status != "" {
		q += " AND d.status=?"
		params = append(params, status)
	}
	if puCode != "" {
		q += " AND d.polling_unit_code=?"
		params = append(params, puCode)
	}
	q += " ORDER BY d.id LIMIT ? OFFSET ?"
	params = append(params, limit, offset)

	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleGetBVASDevice(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	row, err := querySingleRow("SELECT d.*, pu.name as pu_name FROM bvas_devices d LEFT JOIN polling_units pu ON pu.code=d.polling_unit_code WHERE d.id=?", id)
	if err != nil {
		writeError(w, 404, "Device not found")
		return
	}

	var accCount int
	db.QueryRow("SELECT COUNT(*) FROM bvas_accreditations WHERE device_id=?", id).Scan(&accCount)
	row["accreditation_count"] = accCount

	writeJSON(w, 200, row)
}

func handleRegisterBVASDevice(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	var req struct {
		SerialNumber    string  `json:"serial_number"`
		PollingUnitCode string  `json:"polling_unit_code"`
		ElectionID      int     `json:"election_id"`
		Latitude        float64 `json:"latitude"`
		Longitude       float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid JSON"); return }

	var count int
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE serial_number=?", req.SerialNumber).Scan(&count)
	if count > 0 {
		writeError(w, 400, "Device with this serial number already registered")
		return
	}

	var maxID int
	db.QueryRow("SELECT COALESCE(MAX(CAST(SUBSTR(id, 6) AS INTEGER)), 0) FROM bvas_devices").Scan(&maxID)
	devID := fmt.Sprintf("BVAS-%05d", maxID+1)

	db.Exec(`INSERT INTO bvas_devices (id, serial_number, polling_unit_code, election_id, status, latitude, longitude) VALUES (?,?,?,?,'registered',?,?)`,
		devID, req.SerialNumber, req.PollingUnitCode, req.ElectionID, req.Latitude, req.Longitude)
	auditWrite("BVAS_DEVICE_REGISTERED", "bvas_device", devID, r, map[string]interface{}{"serial": req.SerialNumber, "pu_code": req.PollingUnitCode})

	writeJSON(w, 200, M{"id": devID, "message": "BVAS device registered"})
}

func handleUpdateBVASDevice(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin"); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	id := mux.Vars(r)["id"]
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid JSON"); return }

	var updates []string
	var vals []interface{}
	for _, f := range []string{"status", "battery_level", "polling_unit_code", "firmware_version"} {
		if v, ok := req[f]; ok {
			updates = append(updates, f+"=?")
			vals = append(vals, v)
		}
	}
	if len(updates) == 0 {
		writeError(w, 400, "No fields to update")
		return
	}
	vals = append(vals, id)
	db.Exec("UPDATE bvas_devices SET "+strings.Join(updates, ",")+",last_sync_at=CURRENT_TIMESTAMP WHERE id=?", vals...)
	auditWrite("BVAS_DEVICE_UPDATED", "bvas_device", id, r, req)
	writeJSON(w, 200, M{"message": "Device updated"})
}

func handleBVASAccreditation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID        string `json:"device_id"`
		ElectionID      int    `json:"election_id"`
		PollingUnitCode string `json:"polling_unit_code"`
		VoterPVCNumber  string `json:"voter_pvc_number"`
		BiometricMatch  bool   `json:"biometric_match"`
		PVCVerified     bool   `json:"pvc_verified"`
		Method          string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid JSON"); return }
	if req.Method == "" {
		req.Method = "biometric"
	}

	h := sha256.Sum256([]byte(req.VoterPVCNumber))
	pvcHash := hex.EncodeToString(h[:])

	var dupCount int
	db.QueryRow("SELECT COUNT(*) FROM bvas_accreditations WHERE voter_pvc_hash=? AND election_id=? AND polling_unit_code=?",
		pvcHash, req.ElectionID, req.PollingUnitCode).Scan(&dupCount)
	if dupCount > 0 {
		writeError(w, 400, "Voter already accredited at this polling unit")
		return
	}

	lid := insertReturningID(db, `INSERT INTO bvas_accreditations (device_id, election_id, polling_unit_code, voter_pvc_hash, biometric_match, pvc_verified, method, synced_at)
		VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		req.DeviceID, req.ElectionID, req.PollingUnitCode, pvcHash,
		boolToInt(req.BiometricMatch), boolToInt(req.PVCVerified), req.Method)

	db.Exec("UPDATE bvas_devices SET last_sync_at=CURRENT_TIMESTAMP WHERE id=?", req.DeviceID)
	auditWrite("BVAS_ACCREDITATION", "bvas_accreditation", fmt.Sprintf("%d", lid), r, map[string]interface{}{"device_id": req.DeviceID, "pu_code": req.PollingUnitCode, "biometric_match": req.BiometricMatch})

	go broadcastWS(M{"type": "bvas_accreditation", "pu_code": req.PollingUnitCode, "device_id": req.DeviceID, "election_id": req.ElectionID})

	go publishResultEvent("inec.bvas.accreditation", lid, req.PollingUnitCode, req.ElectionID, 0,
		map[string]interface{}{"device_id": req.DeviceID, "method": req.Method, "biometric_match": req.BiometricMatch})

	writeJSON(w, 200, M{"id": lid, "message": "Accreditation recorded", "pvc_hash": pvcHash[:16] + "..."})
}

func handleBVASAccreditationFeed(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	puCode := r.URL.Query().Get("polling_unit_code")
	limit := queryParamInt(r, "limit", 50)

	q := `SELECT a.id, a.device_id, a.polling_unit_code, a.biometric_match, a.pvc_verified, a.method, a.accredited_at,
		pu.name as pu_name FROM bvas_accreditations a LEFT JOIN polling_units pu ON pu.code=a.polling_unit_code
		WHERE a.election_id=?`
	params := []interface{}{eid}
	if puCode != "" {
		q += " AND a.polling_unit_code=?"
		params = append(params, puCode)
	}
	q += " ORDER BY a.accredited_at DESC LIMIT ?"
	params = append(params, limit)

	rows, _ := db.Query(q, params...)
	writeJSON(w, 200, scanRows(rows))
}

func handleBVASReconciliation(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	flagOnly := r.URL.Query().Get("flagged_only") == "true"

	rows, _ := db.Query(`
		SELECT 
			pu.code as polling_unit_code,
			pu.name as pu_name,
			COALESCE(ba.bvas_count, 0) as bvas_accredited,
			COALESCE(r.accredited_voters, 0) as result_accredited,
			COALESCE(ba.bio_pass, 0) as bio_pass_count,
			COALESCE(ba.pvc_pass, 0) as pvc_pass_count,
			COALESCE(ba.bvas_count, 0) as total_bvas
		FROM polling_units pu
		LEFT JOIN (
			SELECT polling_unit_code, COUNT(*) as bvas_count,
				SUM(biometric_match) as bio_pass, SUM(pvc_verified) as pvc_pass
			FROM bvas_accreditations WHERE election_id=? GROUP BY polling_unit_code
		) ba ON ba.polling_unit_code=pu.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		WHERE (ba.bvas_count > 0 OR r.id IS NOT NULL)
		ORDER BY pu.code
	`, eid, eid)
	allRows := scanRows(rows)

	var reconciliations []M
	totalFlags := 0
	for _, row := range allRows {
		bvasAcc := toInt(row["bvas_accredited"])
		resultAcc := toInt(row["result_accredited"])
		discrepancy := abs(bvasAcc - resultAcc)
		discPct := 0.0
		if bvasAcc > 0 {
			discPct = float64(discrepancy) / float64(bvasAcc) * 100
		}
		flagLevel := "none"
		if discPct > 20 {
			flagLevel = "critical"
			totalFlags++
		} else if discPct > 10 {
			flagLevel = "warning"
			totalFlags++
		} else if discPct > 5 {
			flagLevel = "minor"
		}

		bioRate := 0.0
		pvcRate := 0.0
		totalBvas := toInt(row["total_bvas"])
		if totalBvas > 0 {
			bioRate = float64(toInt(row["bio_pass_count"])) / float64(totalBvas) * 100
			pvcRate = float64(toInt(row["pvc_pass_count"])) / float64(totalBvas) * 100
		}

		if flagOnly && flagLevel == "none" {
			continue
		}

		reconciliations = append(reconciliations, M{
			"polling_unit_code":   row["polling_unit_code"],
			"pu_name":             row["pu_name"],
			"bvas_accredited":     bvasAcc,
			"result_accredited":   resultAcc,
			"discrepancy":         discrepancy,
			"discrepancy_pct":     round2(discPct),
			"flag_level":          flagLevel,
			"biometric_pass_rate": round2(bioRate),
			"pvc_verify_rate":     round2(pvcRate),
		})
	}

	writeJSON(w, 200, M{
		"election_id":    eid,
		"total_pus":      len(reconciliations),
		"total_flagged":  totalFlags,
		"reconciliation": reconciliations,
	})
}

func handleBVASSummary(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)

	var totalDevices, activeDevices, offlineDevices, faultyDevices int
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE election_id=?", eid).Scan(&totalDevices)
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE election_id=? AND status='active'", eid).Scan(&activeDevices)
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE election_id=? AND status='offline'", eid).Scan(&offlineDevices)
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE election_id=? AND status='faulty'", eid).Scan(&faultyDevices)

	var totalAcc, bioMatch, pvcVerified int
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(biometric_match),0), COALESCE(SUM(pvc_verified),0) FROM bvas_accreditations WHERE election_id=?", eid).Scan(&totalAcc, &bioMatch, &pvcVerified)

	var avgBattery sql.NullFloat64
	db.QueryRow("SELECT AVG(battery_level) FROM bvas_devices WHERE election_id=? AND status IN ('active','deployed')", eid).Scan(&avgBattery)

	var lowBattery int
	db.QueryRow("SELECT COUNT(*) FROM bvas_devices WHERE election_id=? AND battery_level < 20", eid).Scan(&lowBattery)

	var flaggedPUs int
	db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT pu.code, COALESCE(ba.cnt, 0) as bvas_cnt, COALESCE(r.accredited_voters, 0) as result_cnt
		FROM polling_units pu
		LEFT JOIN (SELECT polling_unit_code, COUNT(*) as cnt FROM bvas_accreditations WHERE election_id=? GROUP BY polling_unit_code) ba ON ba.polling_unit_code=pu.code
		LEFT JOIN results r ON r.polling_unit_code=pu.code AND r.election_id=?
		WHERE ba.cnt > 0 AND r.id IS NOT NULL AND ABS(ba.cnt - r.accredited_voters) > ba.cnt * 0.1
	)`, eid, eid).Scan(&flaggedPUs)

	bioRate := 0.0
	pvcRate := 0.0
	if totalAcc > 0 {
		bioRate = float64(bioMatch) / float64(totalAcc) * 100
		pvcRate = float64(pvcVerified) / float64(totalAcc) * 100
	}

	batteryAvg := 0.0
	if avgBattery.Valid {
		batteryAvg = avgBattery.Float64
	}

	stateRows, _ := db.Query(`
		SELECT s.code, s.name, COUNT(DISTINCT d.id) as device_count,
			SUM(CASE WHEN d.status='active' THEN 1 ELSE 0 END) as active_count,
			COALESCE(ba.acc_count, 0) as accreditation_count
		FROM states s
		LEFT JOIN lgas l ON l.state_code=s.code
		LEFT JOIN wards w ON w.lga_code=l.code
		LEFT JOIN polling_units pu ON pu.ward_code=w.code
		LEFT JOIN bvas_devices d ON d.polling_unit_code=pu.code AND d.election_id=?
		LEFT JOIN (
			SELECT l2.state_code, COUNT(*) as acc_count
			FROM bvas_accreditations a
			JOIN polling_units pu2 ON pu2.code=a.polling_unit_code
			JOIN wards w2 ON w2.code=pu2.ward_code
			JOIN lgas l2 ON l2.code=w2.lga_code
			WHERE a.election_id=?
			GROUP BY l2.state_code
		) ba ON ba.state_code=s.code
		WHERE d.id IS NOT NULL
		GROUP BY s.code, s.name, ba.acc_count ORDER BY accreditation_count DESC
	`, eid, eid)
	stateBreakdown := scanRows(stateRows)

	writeJSON(w, 200, M{
		"election_id": eid,
		"devices": M{
			"total": totalDevices, "active": activeDevices, "offline": offlineDevices, "faulty": faultyDevices,
			"avg_battery": round2(batteryAvg), "low_battery_count": lowBattery,
		},
		"accreditation": M{
			"total": totalAcc, "biometric_match": bioMatch, "pvc_verified": pvcVerified,
			"biometric_pass_rate": round2(bioRate), "pvc_verify_rate": round2(pvcRate),
		},
		"reconciliation": M{
			"flagged_pus": flaggedPUs,
		},
		"state_breakdown": stateBreakdown,
	})
}

func handleBVASAccreditationTimeline(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	interval := queryParam(r, "interval", "hour")

	pgFmtMap:= map[string]string{"minute": "YYYY-MM-DD HH24:MI", "hour": "YYYY-MM-DD HH24:00", "day": "YYYY-MM-DD"}
	pgFmt := pgFmtMap[interval]
	if pgFmt == "" {
		pgFmt = pgFmtMap["hour"]
	}
	rows, _ := db.Query(fmt.Sprintf(`
		SELECT to_char(accredited_at, '%s') as time_bucket,
			COUNT(*) as accreditations,
			SUM(biometric_match) as bio_pass,
			SUM(pvc_verified) as pvc_pass
		FROM bvas_accreditations WHERE election_id=?
		GROUP BY time_bucket ORDER BY time_bucket
	`, pgFmt), eid)
	data := scanRows(rows)

	cumulative := 0
	for i, d := range data {
		cumulative += toInt(d["accreditations"])
		data[i]["cumulative"] = cumulative
	}

	writeJSON(w, 200, M{"election_id": eid, "interval": interval, "data": data})
}

// ── Helpers ──

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
