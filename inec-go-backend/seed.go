package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
)

type stateInfo struct {
	Code    string
	Name    string
	GeoZone string
	Capital string
}

type partyInfo struct {
	Code         string
	Name         string
	Abbreviation string
	Color        string
}

var nigeriaStates = []stateInfo{
	{"AB", "Abia", "South East", "Umuahia"},
	{"AD", "Adamawa", "North East", "Yola"},
	{"AK", "Akwa Ibom", "South South", "Uyo"},
	{"AN", "Anambra", "South East", "Awka"},
	{"BA", "Bauchi", "North East", "Bauchi"},
	{"BY", "Bayelsa", "South South", "Yenagoa"},
	{"BE", "Benue", "North Central", "Makurdi"},
	{"BO", "Borno", "North East", "Maiduguri"},
	{"CR", "Cross River", "South South", "Calabar"},
	{"DE", "Delta", "South South", "Asaba"},
	{"EB", "Ebonyi", "South East", "Abakaliki"},
	{"ED", "Edo", "South South", "Benin City"},
	{"EK", "Ekiti", "South West", "Ado-Ekiti"},
	{"EN", "Enugu", "South East", "Enugu"},
	{"FC", "FCT", "North Central", "Abuja"},
	{"GO", "Gombe", "North East", "Gombe"},
	{"IM", "Imo", "South East", "Owerri"},
	{"JI", "Jigawa", "North West", "Dutse"},
	{"KD", "Kaduna", "North West", "Kaduna"},
	{"KN", "Kano", "North West", "Kano"},
	{"KT", "Katsina", "North West", "Katsina"},
	{"KE", "Kebbi", "North West", "Birnin Kebbi"},
	{"KO", "Kogi", "North Central", "Lokoja"},
	{"KW", "Kwara", "North Central", "Ilorin"},
	{"LA", "Lagos", "South West", "Ikeja"},
	{"NA", "Nasarawa", "North Central", "Lafia"},
	{"NI", "Niger", "North Central", "Minna"},
	{"OG", "Ogun", "South West", "Abeokuta"},
	{"ON", "Ondo", "South West", "Akure"},
	{"OS", "Osun", "South West", "Osogbo"},
	{"OY", "Oyo", "South West", "Ibadan"},
	{"PL", "Plateau", "North Central", "Jos"},
	{"RI", "Rivers", "South South", "Port Harcourt"},
	{"SO", "Sokoto", "North West", "Sokoto"},
	{"TA", "Taraba", "North East", "Jalingo"},
	{"YO", "Yobe", "North East", "Damaturu"},
	{"ZA", "Zamfara", "North West", "Gusau"},
}

var parties = []partyInfo{
	{"APC", "All Progressives Congress", "APC", "#0066CC"},
	{"PDP", "Peoples Democratic Party", "PDP", "#CC0000"},
	{"LP", "Labour Party", "LP", "#009933"},
	{"NNPP", "New Nigeria Peoples Party", "NNPP", "#FF6600"},
	{"ADC", "African Democratic Congress", "ADC", "#9933CC"},
	{"SDP", "Social Democratic Party", "SDP", "#FFD700"},
	{"APGA", "All Progressives Grand Alliance", "APGA", "#228B22"},
	{"YPP", "Young Progressives Party", "YPP", "#FF1493"},
}

var sampleLGAs = map[string][]string{
	"LA": {"Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry", "Epe", "Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe", "Lagos Island", "Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere"},
	"FC": {"Abaji", "Bwari", "Gwagwalada", "Kuje", "Kwali", "Municipal"},
	"KN": {"Ajingi", "Albasu", "Bagwai", "Bebeji", "Bichi", "Bunkure", "Dala", "Dambatta", "Dawakin Kudu", "Dawakin Tofa", "Doguwa", "Fagge", "Gabasawa", "Garko", "Garun Mallam", "Gaya", "Gezawa", "Gwale", "Gwarzo", "Kabo", "Kano Municipal", "Karaye", "Kibiya", "Kiru", "Kumbotso", "Kunchi", "Kura", "Madobi", "Makoda", "Minjibir", "Nassarawa", "Rano", "Rimin Gado", "Rogo", "Shanono", "Sumaila", "Takai", "Tarauni", "Tofa", "Tsanyawa", "Tudun Wada", "Ungogo", "Warawa", "Wudil"},
	"RI": {"Abua/Odual", "Ahoada East", "Ahoada West", "Akuku Toru", "Andoni", "Asari-Toru", "Bonny", "Degema", "Emohua", "Eleme", "Etche", "Gokana", "Ikwerre", "Khana", "Obia/Akpor", "Ogba/Egbema/Ndoni", "Ogu/Bolo", "Okrika", "Omuma", "Opobo/Nkoro", "Oyigbo", "Port Harcourt", "Tai"},
	"OY": {"Afijio", "Akinyele", "Atiba", "Atisbo", "Egbeda", "Ibadan North", "Ibadan North East", "Ibadan North West", "Ibadan South East", "Ibadan South West", "Ibarapa Central", "Ibarapa East", "Ibarapa North", "Ido", "Irepo", "Iseyin", "Itesiwaju", "Iwajowa", "Kajola", "Lagelu", "Ogbomoso North", "Ogbomoso South", "Ogo Oluwa", "Oluyole", "Ona Ara", "Orelope", "Ori Ire", "Oyo East", "Oyo West", "Saki East", "Saki West", "Surulere"},
	"AB": {"Aba North", "Aba South", "Arochukwu", "Bende", "Ikwuano", "Isiala Ngwa North", "Isiala Ngwa South", "Isuikwuato", "Obi Ngwa", "Ohafia", "Osisioma Ngwa", "Ugwunagbo", "Ukwa East", "Ukwa West", "Umuahia North", "Umuahia South", "Umu Nneochi"},
	"KD": {"Birnin Gwari", "Chikun", "Giwa", "Igabi", "Ikara", "Jaba", "Jema'a", "Kachia", "Kaduna North", "Kaduna South", "Kagarko", "Kajuru", "Kaura", "Kauru", "Kubau", "Kudan", "Lere", "Makarfi", "Sabon Gari", "Sanga", "Soba", "Zangon Kataf", "Zaria"},
}

var wardNames = []string{"Ward I", "Ward II", "Ward III", "Ward IV", "Ward V", "Ward VI", "Ward VII", "Ward VIII", "Ward IX", "Ward X", "Ward XI", "Ward XII"}

func seedDatabase(db *sql.DB) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM states").Scan(&count)
	if count > 0 {
		return
	}

	tx, _ := db.Begin()

	for _, s := range nigeriaStates {
		tx.Exec("INSERT INTO states (code, name, geo_zone, capital) VALUES (?,?,?,?)", s.Code, s.Name, s.GeoZone, s.Capital)
	}
	for _, p := range parties {
		tx.Exec("INSERT INTO parties (code, name, abbreviation, color) VALUES (?,?,?,?)", p.Code, p.Name, p.Abbreviation, p.Color)
	}

	adminHash := hashPassword("admin123")
	tx.Exec("INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?,?,?,?,?)",
		"admin", adminHash, "System Administrator", "admin", "INEC-ADMIN-001")
	observerHash := hashPassword("observer123")
	tx.Exec("INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?,?,?,?,?)",
		"observer", observerHash, "Election Observer", "observer", "OBS-001")

	puCounter := 0
	for _, state := range nigeriaStates {
		lgaNames, ok := sampleLGAs[state.Code]
		if !ok {
			n := rand.Intn(13) + 8
			lgaNames = make([]string, n)
			for i := range lgaNames {
				lgaNames[i] = fmt.Sprintf("LGA-%d", i+1)
			}
		}
		for idx, lgaName := range lgaNames {
			lgaCode := fmt.Sprintf("%s-%03d", state.Code, idx+1)
			tx.Exec("INSERT INTO lgas (code, name, state_code) VALUES (?,?,?)", lgaCode, lgaName, state.Code)

			numWards := rand.Intn(8) + 8
			for wIdx := 0; wIdx < numWards; wIdx++ {
				wardCode := fmt.Sprintf("%s-W%03d", lgaCode, wIdx+1)
				wardName := wardNames[wIdx%len(wardNames)]
				if wIdx >= len(wardNames) {
					wardName = fmt.Sprintf("Ward %d", wIdx+1)
				}
				tx.Exec("INSERT INTO wards (code, name, lga_code) VALUES (?,?,?)", wardCode, wardName, lgaCode)

				numPUs := rand.Intn(6) + 3
				for pIdx := 0; pIdx < numPUs; pIdx++ {
					puCode := fmt.Sprintf("%s-PU%03d", wardCode, pIdx+1)
					puName := fmt.Sprintf("Polling Unit %d", pIdx+1)
					registered := rand.Intn(1001) + 200
					lat := 4.0 + rand.Float64()*10
					lng := 2.5 + rand.Float64()*12
					tx.Exec("INSERT INTO polling_units (code, name, ward_code, registered_voters, latitude, longitude) VALUES (?,?,?,?,?,?)",
						puCode, puName, wardCode, registered, fmt.Sprintf("%.6f", lat), fmt.Sprintf("%.6f", lng))
					puCounter++
				}
			}
		}
	}

	officerHash := hashPassword("officer123")
	tx.Exec("INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code, polling_unit_code) VALUES (?,?,?,?,?,?,?)",
		"officer1", officerHash, "Adeola Bakare", "presiding_officer", "INEC-LA-00234", "LA", "LA-001-W001-PU001")

	tx.Exec(`INSERT INTO elections (title, election_type, election_date, status, description, total_registered_voters) VALUES (?,?,?,?,?,?)`,
		"2027 Presidential Election", "presidential", "2027-02-28", "active",
		"General election for the President of the Federal Republic of Nigeria", puCounter*500)
	tx.Commit()

	var electionID int
	db.QueryRow("SELECT id FROM elections LIMIT 1").Scan(&electionID)

	rows, err := db.Query("SELECT code FROM polling_units ORDER BY RANDOM() LIMIT 800")
	if err != nil {
		log.Printf("seedDatabase: polling_units query failed: %v", err)
		return
	}
	var samplePUs []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		samplePUs = append(samplePUs, c)
	}
	rows.Close()

	partyCodes := make([]string, len(parties))
	for i, p := range parties {
		partyCodes[i] = p.Code
	}

	tx2, _ := db.Begin()
	for _, puCode := range samplePUs {
		var registered int
		db.QueryRow("SELECT registered_voters FROM polling_units WHERE code=?", puCode).Scan(&registered)
		accredited := int(float64(registered) * (0.4 + rand.Float64()*0.45))
		rejected := rand.Intn(int(float64(accredited)*0.05) + 1)
		validVotes := accredited - rejected

		weights := make([]float64, len(partyCodes))
		totalW := 0.0
		for i := range weights {
			weights[i] = rand.Float64()
			totalW += weights[i]
		}
		partyVotes := make(map[string]int)
		remaining := validVotes
		for i, pc := range partyCodes {
			if i == len(partyCodes)-1 {
				partyVotes[pc] = remaining
			} else {
				v := int(float64(validVotes) * weights[i] / totalW)
				partyVotes[pc] = v
				remaining -= v
			}
		}

		tbID := fmt.Sprintf("TB-%06d", rand.Intn(900000)+100000)
		hlID := fmt.Sprintf("0x%x", rand.Int63())
		ipfsCid := fmt.Sprintf("Qm%x", rand.Int63())
		ec8aHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(puCode)))

		statuses := []string{"finalized", "finalized", "finalized", "finalized", "finalized", "finalized", "finalized", "validated", "validated", "pending"}
		status := statuses[rand.Intn(len(statuses))]
		tbStatus := "PENDING"
		hlStatus := "PENDING"
		if status == "finalized" {
			tbStatus = "POSTED"
			hlStatus = "CONFIRMED"
		}

		resultID := insertReturningID(tx2, `INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
			total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
			ec8a_hash, tigerbeetle_transfer_id, hyperledger_tx_id, tigerbeetle_status, hyperledger_status, ipfs_cid)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			electionID, puCode, 3, status, validVotes, rejected, accredited, accredited,
			ec8aHash, tbID, hlID, tbStatus, hlStatus, ipfsCid)

		for pc, votes := range partyVotes {
			tx2.Exec("INSERT INTO result_party_scores (result_id, party_code, votes) VALUES (?,?,?)", resultID, pc, votes)
		}
	}

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"
	actions := []string{"RESULT_SUBMITTED", "RESULT_VALIDATED", "RESULT_FINALIZED", "ELECTION_CREATED", "USER_LOGIN"}
	for i := 0; i < 50; i++ {
		action := actions[rand.Intn(len(actions))]
		blockData := fmt.Sprintf("%s%s%d", prevHash, action, i)
		h := sha256.Sum256([]byte(blockData))
		blockHash := hex.EncodeToString(h[:])
		entityType := "system"
		if len(action) > 6 && action[:6] == "RESULT" {
			entityType = "result"
		}
		phase := []string{"Pre-Validation", "Edge Validation", "Result Submission", "Finalization"}[rand.Intn(4)]
		detailsJSON, _ := json.Marshal(map[string]string{"phase": phase})
		tx2.Exec("INSERT INTO audit_log (action, entity_type, entity_id, user_id, details, block_hash, prev_block_hash) VALUES (?,?,?,?,?,?,?)",
			action, entityType, fmt.Sprintf("%d", rand.Intn(800)+1), []int{1, 2, 3}[rand.Intn(3)], string(detailsJSON), blockHash, prevHash)
		prevHash = blockHash
	}
	tx2.Commit()

	seedBVASDevices(db)
}
