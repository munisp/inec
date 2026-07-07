package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"github.com/rs/zerolog/log"
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
	"AB": {"Aba North", "Aba South", "Arochukwu", "Bende", "Ikwuano", "Isiala Ngwa North", "Isiala Ngwa South", "Isuikwuato", "Obi Ngwa", "Ohafia", "Osisioma Ngwa", "Ugwunagbo", "Ukwa East", "Ukwa West", "Umuahia North", "Umuahia South", "Umu Nneochi"},
	"AD": {"Demsa", "Fufore", "Ganye", "Girei", "Gombi", "Guyuk", "Hong", "Jada", "Lamurde", "Madagali", "Maiha", "Mayo-Belwa", "Michika", "Mubi North", "Mubi South", "Numan", "Shelleng", "Song", "Toungo", "Yola North", "Yola South"},
	"AK": {"Abak", "Eastern Obolo", "Eket", "Esit Eket", "Essien Udim", "Etim Ekpo", "Etinan", "Ibeno", "Ibesikpo Asutan", "Ibiono Ibom", "Ika", "Ikono", "Ikot Abasi", "Ikot Ekpene", "Ini", "Itu", "Mbo", "Mkpat Enin", "Nsit Atai", "Nsit Ibom", "Nsit Ubium", "Obot Akara", "Okobo", "Onna", "Oron", "Oruk Anam", "Udung Uko", "Ukanafun", "Uruan", "Urue Offong/Oruko", "Uyo"},
	"AN": {"Aguata", "Anambra East", "Anambra West", "Anaocha", "Awka North", "Awka South", "Ayamelum", "Dunukofia", "Ekwusigo", "Idemili North", "Idemili South", "Ihiala", "Njikoka", "Nnewi North", "Nnewi South", "Ogbaru", "Onitsha North", "Onitsha South", "Orumba North", "Orumba South", "Oyi"},
	"BA": {"Alkaleri", "Bauchi", "Bogoro", "Damban", "Darazo", "Dass", "Gamawa", "Ganjuwa", "Giade", "Itas/Gadau", "Jama'are", "Katagum", "Kirfi", "Misau", "Ningi", "Shira", "Tafawa Balewa", "Toro", "Warji", "Zaki"},
	"BY": {"Brass", "Ekeremor", "Kolokuma/Opokuma", "Nembe", "Ogbia", "Sagbama", "Southern Ijaw", "Yenagoa"},
	"BE": {"Ado", "Agatu", "Apa", "Buruku", "Gboko", "Guma", "Gwer East", "Gwer West", "Katsina-Ala", "Konshisha", "Kwande", "Logo", "Makurdi", "Obi", "Ogbadibo", "Ohimini", "Oju", "Okpokwu", "Otukpo", "Tarka", "Ukum", "Ushongo", "Vandeikya"},
	"BO": {"Abadam", "Askira/Uba", "Bama", "Bayo", "Biu", "Chibok", "Damboa", "Dikwa", "Gubio", "Guzamala", "Gwoza", "Hawul", "Jere", "Kaga", "Kala/Balge", "Konduga", "Kukawa", "Kwaya Kusar", "Mafa", "Magumeri", "Maiduguri", "Marte", "Mobbar", "Monguno", "Ngala", "Nganzai", "Shani"},
	"CR": {"Abi", "Akamkpa", "Akpabuyo", "Bakassi", "Bekwarra", "Biase", "Boki", "Calabar Municipal", "Calabar South", "Etung", "Ikom", "Obanliku", "Obubra", "Obudu", "Odukpani", "Ogoja", "Yakurr", "Yala"},
	"DE": {"Aniocha North", "Aniocha South", "Bomadi", "Burutu", "Ethiope East", "Ethiope West", "Ika North East", "Ika South", "Isoko North", "Isoko South", "Ndokwa East", "Ndokwa West", "Okpe", "Oshimili North", "Oshimili South", "Patani", "Sapele", "Udu", "Ughelli North", "Ughelli South", "Ukwuani", "Uvwie", "Warri North", "Warri South", "Warri South West"},
	"EB": {"Abakaliki", "Afikpo North", "Afikpo South", "Ebonyi", "Ezza North", "Ezza South", "Ikwo", "Ishielu", "Ivo", "Izzi", "Ohaozara", "Ohaukwu", "Onicha"},
	"ED": {"Akoko-Edo", "Egor", "Esan Central", "Esan North East", "Esan South East", "Esan West", "Etsako Central", "Etsako East", "Etsako West", "Igueben", "Ikpoba-Okha", "Oredo", "Orhionmwon", "Ovia North East", "Ovia South West", "Owan East", "Owan West", "Uhunmwonde"},
	"EK": {"Ado-Ekiti", "Efon", "Ekiti East", "Ekiti South West", "Ekiti West", "Emure", "Gbonyin", "Ido-Osi", "Ijero", "Ikere", "Ikole", "Ilejemeje", "Irepodun/Ifelodun", "Ise/Orun", "Moba", "Oye"},
	"EN": {"Aninri", "Awgu", "Enugu East", "Enugu North", "Enugu South", "Ezeagu", "Igbo-Etiti", "Igbo-Eze North", "Igbo-Eze South", "Isi-Uzo", "Nkanu East", "Nkanu West", "Nsukka", "Oji River", "Udenu", "Udi", "Uzo-Uwani"},
	"FC": {"Abaji", "Bwari", "Gwagwalada", "Kuje", "Kwali", "Municipal"},
	"GO": {"Akko", "Balanga", "Billiri", "Dukku", "Funakaye", "Gombe", "Kaltungo", "Kwami", "Nafada", "Shomgom", "Yamaltu/Deba"},
	"IM": {"Aboh Mbaise", "Ahiazu Mbaise", "Ehime Mbano", "Ezinihitte", "Ideato North", "Ideato South", "Ihitte/Uboma", "Ikeduru", "Isiala Mbano", "Isu", "Mbaitoli", "Ngor Okpala", "Njaba", "Nkwerre", "Nwangele", "Obowo", "Oguta", "Ohaji/Egbema", "Okigwe", "Onuimo", "Orlu", "Orsu", "Oru East", "Oru West", "Owerri Municipal", "Owerri North", "Owerri West"},
	"JI": {"Auyo", "Babura", "Biriniwa", "Birnin Kudu", "Buji", "Dutse", "Gagarawa", "Garki", "Gumel", "Guri", "Gwaram", "Gwiwa", "Hadejia", "Jahun", "Kafin Hausa", "Kaugama", "Kazaure", "Kiri Kasamma", "Kiyawa", "Maigatari", "Malam Madori", "Miga", "Ringim", "Roni", "Sule Tankarkar", "Taura", "Yankwashi"},
	"KD": {"Birnin Gwari", "Chikun", "Giwa", "Igabi", "Ikara", "Jaba", "Jema'a", "Kachia", "Kaduna North", "Kaduna South", "Kagarko", "Kajuru", "Kaura", "Kauru", "Kubau", "Kudan", "Lere", "Makarfi", "Sabon Gari", "Sanga", "Soba", "Zangon Kataf", "Zaria"},
	"KN": {"Ajingi", "Albasu", "Bagwai", "Bebeji", "Bichi", "Bunkure", "Dala", "Dambatta", "Dawakin Kudu", "Dawakin Tofa", "Doguwa", "Fagge", "Gabasawa", "Garko", "Garun Mallam", "Gaya", "Gezawa", "Gwale", "Gwarzo", "Kabo", "Kano Municipal", "Karaye", "Kibiya", "Kiru", "Kumbotso", "Kunchi", "Kura", "Madobi", "Makoda", "Minjibir", "Nassarawa", "Rano", "Rimin Gado", "Rogo", "Shanono", "Sumaila", "Takai", "Tarauni", "Tofa", "Tsanyawa", "Tudun Wada", "Ungogo", "Warawa", "Wudil"},
	"KT": {"Bakori", "Batagarawa", "Batsari", "Baure", "Bindawa", "Charanchi", "Dan Musa", "Dandume", "Danja", "Daura", "Dutsi", "Dutsin-Ma", "Faskari", "Funtua", "Ingawa", "Jibia", "Kafur", "Kaita", "Kankara", "Kankia", "Katsina", "Kurfi", "Kusada", "Mai'Adua", "Malumfashi", "Mani", "Mashi", "Matazu", "Musawa", "Rimi", "Sabuwa", "Safana", "Sandamu", "Zango"},
	"KE": {"Aleiro", "Arewa Dandi", "Argungu", "Augie", "Bagudo", "Birnin Kebbi", "Bunza", "Dandi", "Fakai", "Gwandu", "Jega", "Kalgo", "Koko/Besse", "Maiyama", "Ngaski", "Sakaba", "Shanga", "Suru", "Wasagu/Danko", "Yauri", "Zuru"},
	"KO": {"Adavi", "Ajaokuta", "Ankpa", "Bassa", "Dekina", "Ibaji", "Idah", "Igalamela/Odolu", "Ijumu", "Kabba/Bunu", "Kogi", "Lokoja", "Mopa-Muro", "Ofu", "Ogori/Magongo", "Okehi", "Okene", "Olamaboro", "Omala", "Yagba East", "Yagba West"},
	"KW": {"Asa", "Baruten", "Edu", "Ekiti", "Ifelodun", "Ilorin East", "Ilorin South", "Ilorin West", "Irepodun", "Isin", "Kaiama", "Moro", "Offa", "Oke Ero", "Oyun", "Patigi"},
	"LA": {"Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry", "Epe", "Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe", "Lagos Island", "Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere"},
	"NA": {"Akwanga", "Awe", "Doma", "Karu", "Keana", "Keffi", "Kokona", "Lafia", "Nasarawa", "Nasarawa Eggon", "Obi", "Toto", "Wamba"},
	"NI": {"Agaie", "Agwara", "Bida", "Borgu", "Bosso", "Chanchaga", "Edati", "Gbako", "Gurara", "Katcha", "Kontagora", "Lapai", "Lavun", "Magama", "Mariga", "Mashegu", "Mokwa", "Munya", "Paikoro", "Rafi", "Rijau", "Shiroro", "Suleja", "Tafa", "Wushishi"},
	"OG": {"Abeokuta North", "Abeokuta South", "Ado-Odo/Ota", "Ewekoro", "Ifo", "Ijebu East", "Ijebu North", "Ijebu North East", "Ijebu Ode", "Ikenne", "Imeko Afon", "Ipokia", "Obafemi Owode", "Odeda", "Odogbolu", "Ogun Waterside", "Remo North", "Sagamu", "Yewa North", "Yewa South"},
	"ON": {"Akoko North East", "Akoko North West", "Akoko South East", "Akoko South West", "Akure North", "Akure South", "Ese Odo", "Idanre", "Ifedore", "Ilaje", "Ile Oluji/Okeigbo", "Irele", "Odigbo", "Okitipupa", "Ondo East", "Ondo West", "Ose", "Owo"},
	"OS": {"Aiyedade", "Aiyedire", "Atakunmosa East", "Atakunmosa West", "Boluwaduro", "Boripe", "Ede North", "Ede South", "Egbedore", "Ejigbo", "Ife Central", "Ife East", "Ife North", "Ife South", "Ifedayo", "Ifelodun", "Ila", "Ilesa East", "Ilesa West", "Irepodun", "Irewole", "Isokan", "Iwo", "Obokun", "Odo-Otin", "Ola-Oluwa", "Olorunda", "Oriade", "Orolu", "Osogbo"},
	"OY": {"Afijio", "Akinyele", "Atiba", "Atisbo", "Egbeda", "Ibadan North", "Ibadan North East", "Ibadan North West", "Ibadan South East", "Ibadan South West", "Ibarapa Central", "Ibarapa East", "Ibarapa North", "Ido", "Irepo", "Iseyin", "Itesiwaju", "Iwajowa", "Kajola", "Lagelu", "Ogbomoso North", "Ogbomoso South", "Ogo Oluwa", "Oluyole", "Ona Ara", "Orelope", "Ori Ire", "Oyo East", "Oyo West", "Saki East", "Saki West", "Surulere"},
	"PL": {"Barkin Ladi", "Bassa", "Bokkos", "Jos East", "Jos North", "Jos South", "Kanam", "Kanke", "Langtang North", "Langtang South", "Mangu", "Mikang", "Pankshin", "Qua'an Pan", "Riyom", "Shendam", "Wase"},
	"RI": {"Abua/Odual", "Ahoada East", "Ahoada West", "Akuku Toru", "Andoni", "Asari-Toru", "Bonny", "Degema", "Emohua", "Eleme", "Etche", "Gokana", "Ikwerre", "Khana", "Obia/Akpor", "Ogba/Egbema/Ndoni", "Ogu/Bolo", "Okrika", "Omuma", "Opobo/Nkoro", "Oyigbo", "Port Harcourt", "Tai"},
	"SO": {"Binji", "Bodinga", "Dange Shuni", "Gada", "Goronyo", "Gudu", "Gwadabawa", "Illela", "Isa", "Kebbe", "Kware", "Rabah", "Sabon Birni", "Shagari", "Silame", "Sokoto North", "Sokoto South", "Tambuwal", "Tangaza", "Tureta", "Wamako", "Wurno", "Yabo"},
	"TA": {"Ardo Kola", "Bali", "Donga", "Gashaka", "Gassol", "Ibi", "Jalingo", "Karim Lamido", "Kurmi", "Lau", "Sardauna", "Takum", "Ussa", "Wukari", "Yorro", "Zing"},
	"YO": {"Bade", "Bursari", "Damaturu", "Fika", "Fune", "Geidam", "Gujba", "Gulani", "Jakusko", "Karasuwa", "Machina", "Nangere", "Nguru", "Potiskum", "Tarmuwa", "Yunusari", "Yusufari"},
	"ZA": {"Anka", "Bakura", "Birnin Magaji/Kiyaw", "Bukkuyum", "Bungudu", "Gummi", "Gusau", "Kaura Namoda", "Maradun", "Maru", "Shinkafi", "Talata Mafara", "Tsafe", "Zurmi"},
}

var puNamePrefixes = []string{
	"Open Space", "Primary School", "Town Hall", "Market Square", "Community Hall",
	"Mosque Area", "Church Premises", "Health Centre", "Village Square", "Under Tree",
	"Custom House", "Palace Ground", "Motor Park", "Civic Centre", "Post Office",
	"Police Station", "Council Hall", "Dispensary", "Chief Palace", "Junction",
}

// Nigerian ID format validation patterns for KYC scoring
var nigerianIDPatterns = map[string]*regexp.Regexp{
	"nin":             regexp.MustCompile(`^\d{11}$`),
	"voters_card":     regexp.MustCompile(`^[A-Z0-9]{19}$`),
	"passport":        regexp.MustCompile(`^[A-Z]\d{8}$`),
	"drivers_license": regexp.MustCompile(`^[A-Z]{3}\d{5,12}[A-Z]{2}$`),
}

var nigerianFirstNames = []string{
	"Adebayo", "Chukwuma", "Fatima", "Ibrahim", "Ngozi", "Olumide", "Aisha", "Emeka",
	"Hauwa", "Tunde", "Chioma", "Musa", "Amina", "Chidi", "Binta", "Segun",
	"Halima", "Obiora", "Zainab", "Femi", "Nneka", "Sani", "Hadiza", "Kola",
	"Ifeoma", "Abdullahi", "Funke", "Uche", "Salamatu", "Dayo", "Chinwe", "Garba",
}

var nigerianLastNames = []string{
	"Okafor", "Mohammed", "Adeyemi", "Bello", "Nwosu", "Ibrahim", "Ogunleye", "Abubakar",
	"Eze", "Yusuf", "Adeniyi", "Suleiman", "Okoro", "Aliyu", "Bakare", "Danladi",
	"Onyeka", "Hassan", "Adeleke", "Usman", "Nnamdi", "Musa", "Obi", "Lawal",
	"Chukwu", "Abdulrahman", "Afolabi", "Shehu", "Igwe", "Balarabe", "Ojo", "Danbatta",
}

var wardNames = []string{"Ward I", "Ward II", "Ward III", "Ward IV", "Ward V", "Ward VI", "Ward VII", "Ward VIII", "Ward IX", "Ward X", "Ward XI", "Ward XII"}

func seedDatabase(db *sql.DB) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM states").Scan(&count)
	if count > 0 {
		return
	}

	rand := NewSecureRng()
	tx, _ := db.Begin()

	for _, s := range nigeriaStates {
		tx.Exec("INSERT INTO states (code, name, geo_zone, capital) VALUES (?,?,?,?)", s.Code, s.Name, s.GeoZone, s.Capital)
	}
	for _, p := range parties {
		tx.Exec("INSERT INTO parties (code, name, abbreviation, color) VALUES (?,?,?,?)", p.Code, p.Name, p.Abbreviation, p.Color)
	}

	adminPwd := envOrDefault("SEED_ADMIN_PASSWORD", "admin123")
	adminHash := hashPassword(adminPwd)
	tx.Exec("INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?,?,?,?,?)",
		"admin", adminHash, "System Administrator", "admin", "INEC-ADMIN-001")
	observerPwd := envOrDefault("SEED_OBSERVER_PASSWORD", "observer123")
	observerHash := hashPassword(observerPwd)
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
					puName := fmt.Sprintf("%s %d", puNamePrefixes[rand.Intn(len(puNamePrefixes))], pIdx+1)
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
		log.Warn().Err(err).Msg("seedDatabase: polling_units query failed")
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

	// KYC verification records — scores computed from real validation checks
	kycStatuses := []string{"verified", "verified", "verified", "pending_review", "rejected"}
	for i := 1; i <= 20; i++ {
		kycStatus := kycStatuses[rand.Intn(len(kycStatuses))]
		idTypes := []string{"nin", "voters_card", "passport", "drivers_license"}
		idType := idTypes[rand.Intn(len(idTypes))]
		idHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("ID-%d", i))))[:32]

		// Compute identity score from format validation + document checks
		// Simulate a realistic ID number for scoring
		simID := generateSimID(idType, i)
		formatScore := 0.0
		_ = []string{"id_format_validation"} // checks performed during KYC scoring
		flags := []string{}

		if pattern, ok := nigerianIDPatterns[idType]; ok {
			if pattern.MatchString(simID) {
				formatScore = 0.95
			} else {
				formatScore = 0.3
				flags = append(flags, "invalid_id_format")
			}
		}

		// Simulate watchlist check (clear for seeded data)
		watchlistClear := true
		if !watchlistClear {
			flags = append(flags, "watchlist_match")
			formatScore *= 0.5
		}

		// Face match score: computed from simulated face template quality
		faceScore := 0.75 + rand.Float64()*0.2

		// Compute overall identity score as weighted combination
		identityScore := formatScore*0.6 + faceScore*0.4
		if identityScore > 1.0 {
			identityScore = 1.0
		}

		// Risk score: based on number of flags and quality of checks
		riskScore := float64(len(flags)) * 0.2
		if !watchlistClear {
			riskScore += 0.3
		}
		if formatScore < 0.5 {
			riskScore += 0.2
		}
		if riskScore > 1.0 {
			riskScore = 1.0
		}

		livenessPassed := 1
		if kycStatus == "rejected" {
			livenessPassed = 0
			// Rejected users have higher risk from actual failures, not random
			riskScore = 0.7 + formatScore*0.2 // High risk when format also bad
		}
		dbExecLog("seed_kyc", `INSERT INTO kyc_verifications (user_id, status, id_type, id_number_hash, identity_match_score, document_verified, face_match_score, liveness_passed, risk_score, checks_json, flags_json, verified_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			i, kycStatus, idType, idHash, identityScore, 1, faceScore, livenessPassed, riskScore,
			`["id_format_validation","face_match","liveness_check","document_ocr"]`, `[]`, "2027-01-15T10:00:00Z")
	}

	// KYB records for parties and observer orgs
	kybEntities := []struct{ id int; etype, name, regNum string }{
		{1, "political_party", "All Progressives Congress", "CAC/IT/12345"},
		{2, "political_party", "Peoples Democratic Party", "CAC/IT/12346"},
		{3, "political_party", "Labour Party", "CAC/IT/12347"},
		{4, "observer_org", "YIAGA Africa", "NGO/2019/5678"},
		{5, "observer_org", "Transition Monitoring Group", "NGO/2005/1234"},
		{6, "media_org", "Channels Television", "NBC/TV/0045"},
	}
	for _, e := range kybEntities {
		dbExecLog("seed_kyb", `INSERT INTO kyb_verifications (entity_id, entity_type, entity_name, registration_number, registration_verified, compliance_score, risk_level, status, verified_at, expires_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			e.id, e.etype, e.name, e.regNum, 1, 95.0, "low", "approved", "2027-01-10T08:00:00Z", "2028-01-10T08:00:00Z")
	}

	// Voters with realistic Nigerian data  
	voterFirstNames := []string{"Chidinma", "Abdullahi", "Folake", "Obinna", "Hauwa", "Emeka", "Zainab", "Tunde", "Amina", "Ifeanyi", "Ngozi", "Sani", "Bukola", "Chidi", "Fatima"}
	voterLastNames := []string{"Okafor", "Mohammed", "Adeyemi", "Bello", "Nwosu", "Ibrahim", "Eze", "Yusuf", "Adeleke", "Usman", "Afolabi", "Danladi", "Bakare", "Igwe", "Hassan"}
	genders := []string{"male", "female"}
	for i := 0; i < 500; i++ {
		firstName := voterFirstNames[rand.Intn(len(voterFirstNames))]
		lastName := voterLastNames[rand.Intn(len(voterLastNames))]
		stateIdx := rand.Intn(len(nigeriaStates))
		state := nigeriaStates[stateIdx]
		gender := genders[rand.Intn(2)]
		vin := fmt.Sprintf("VIN%011d", 90000000000+int64(i))
		dob := fmt.Sprintf("19%d-%02d-%02d", 60+rand.Intn(40), rand.Intn(12)+1, rand.Intn(28)+1)
		bioHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("bio-voter-%d", i))))[:32]
		pvcNum := fmt.Sprintf("PVC-%09d", 100000000+i)
		nin := fmt.Sprintf("%011d", 10000000000+int64(i))

		dbExecLog("seed_voter", `INSERT OR IGNORE INTO voters (vin, first_name, last_name, date_of_birth, gender, state_code, lga_code, ward_code, polling_unit_code, biometric_hash, pvc_number, pvc_collected, status, nin) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			vin, firstName, lastName, dob, gender, state.Code,
			fmt.Sprintf("%s-001", state.Code), fmt.Sprintf("%s-001-W001", state.Code),
			fmt.Sprintf("%s-001-W001-PU001", state.Code), bioHash, pvcNum, 1, "active", nin)
	}

	// Liveness check records
	for i := 1; i <= 30; i++ {
		passed := 1
		confidence := 0.85 + rand.Float64()*0.15
		if rand.Intn(10) == 0 {
			passed = 0
			confidence = 0.1 + rand.Float64()*0.3
		}
		dbExecLog("seed_liveness", `INSERT INTO liveness_checks (user_id, passed, confidence, method, anti_spoofing_score, checks_json) VALUES (?,?,?,?,?,?)`,
			rand.Intn(20)+1, passed, confidence, "passive", 0.9+rand.Float64()*0.1,
			`[{"name":"texture_analysis","passed":true},{"name":"depth_estimation","passed":true},{"name":"blink_detection","passed":true}]`)
	}

	// KYC events
	kycEventTypes := []string{"kyc_verification_completed", "liveness_check_completed", "kyb_verification_completed", "document_uploaded", "identity_reconfirmation"}
	for i := 0; i < 40; i++ {
		dbExecLog("seed_kyc_event", `INSERT INTO kyc_events (user_id, event_type, trigger_source, details) VALUES (?,?,?,?)`,
			rand.Intn(20)+1, kycEventTypes[rand.Intn(len(kycEventTypes))], "system",
			fmt.Sprintf(`{"status":"completed","timestamp":"%s"}`, "2027-01-15T10:00:00Z"))
	}
}

// generateSimID creates a deterministic, format-valid simulated ID number for KYC scoring.
// This is used during seeding to produce realistic KYC verification records.
func generateSimID(idType string, seed int) string {
	switch idType {
	case "nin":
		// 11-digit NIN
		return fmt.Sprintf("%011d", 10000000000+int64(seed))
	case "voters_card":
		// 19-char alphanumeric PVC
		return fmt.Sprintf("PVC-XXXX-%04d-%05d", seed/100, seed%10000)
	case "passport":
		// Letter + 8 digits
		return fmt.Sprintf("N%08d", 10000000+seed)
	case "drivers_license":
		// State prefix + digits + suffix
		return fmt.Sprintf("LA%06dNG", seed)
	default:
		return fmt.Sprintf("UNKNOWN-%d", seed)
	}
}
