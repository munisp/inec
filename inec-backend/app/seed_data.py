import sqlite3
import hashlib
import json
import os
import random
from passlib.hash import pbkdf2_sha256

DB_PATH = os.environ.get("DATABASE_PATH", "/data/app.db")
if not os.path.exists(os.path.dirname(DB_PATH)) and DB_PATH.startswith("/data"):
    DB_PATH = "app.db"

NIGERIA_STATES = [
    {"code": "AB", "name": "Abia", "geo_zone": "South East", "capital": "Umuahia"},
    {"code": "AD", "name": "Adamawa", "geo_zone": "North East", "capital": "Yola"},
    {"code": "AK", "name": "Akwa Ibom", "geo_zone": "South South", "capital": "Uyo"},
    {"code": "AN", "name": "Anambra", "geo_zone": "South East", "capital": "Awka"},
    {"code": "BA", "name": "Bauchi", "geo_zone": "North East", "capital": "Bauchi"},
    {"code": "BY", "name": "Bayelsa", "geo_zone": "South South", "capital": "Yenagoa"},
    {"code": "BE", "name": "Benue", "geo_zone": "North Central", "capital": "Makurdi"},
    {"code": "BO", "name": "Borno", "geo_zone": "North East", "capital": "Maiduguri"},
    {"code": "CR", "name": "Cross River", "geo_zone": "South South", "capital": "Calabar"},
    {"code": "DE", "name": "Delta", "geo_zone": "South South", "capital": "Asaba"},
    {"code": "EB", "name": "Ebonyi", "geo_zone": "South East", "capital": "Abakaliki"},
    {"code": "ED", "name": "Edo", "geo_zone": "South South", "capital": "Benin City"},
    {"code": "EK", "name": "Ekiti", "geo_zone": "South West", "capital": "Ado-Ekiti"},
    {"code": "EN", "name": "Enugu", "geo_zone": "South East", "capital": "Enugu"},
    {"code": "FC", "name": "FCT", "geo_zone": "North Central", "capital": "Abuja"},
    {"code": "GO", "name": "Gombe", "geo_zone": "North East", "capital": "Gombe"},
    {"code": "IM", "name": "Imo", "geo_zone": "South East", "capital": "Owerri"},
    {"code": "JI", "name": "Jigawa", "geo_zone": "North West", "capital": "Dutse"},
    {"code": "KD", "name": "Kaduna", "geo_zone": "North West", "capital": "Kaduna"},
    {"code": "KN", "name": "Kano", "geo_zone": "North West", "capital": "Kano"},
    {"code": "KT", "name": "Katsina", "geo_zone": "North West", "capital": "Katsina"},
    {"code": "KE", "name": "Kebbi", "geo_zone": "North West", "capital": "Birnin Kebbi"},
    {"code": "KO", "name": "Kogi", "geo_zone": "North Central", "capital": "Lokoja"},
    {"code": "KW", "name": "Kwara", "geo_zone": "North Central", "capital": "Ilorin"},
    {"code": "LA", "name": "Lagos", "geo_zone": "South West", "capital": "Ikeja"},
    {"code": "NA", "name": "Nasarawa", "geo_zone": "North Central", "capital": "Lafia"},
    {"code": "NI", "name": "Niger", "geo_zone": "North Central", "capital": "Minna"},
    {"code": "OG", "name": "Ogun", "geo_zone": "South West", "capital": "Abeokuta"},
    {"code": "ON", "name": "Ondo", "geo_zone": "South West", "capital": "Akure"},
    {"code": "OS", "name": "Osun", "geo_zone": "South West", "capital": "Osogbo"},
    {"code": "OY", "name": "Oyo", "geo_zone": "South West", "capital": "Ibadan"},
    {"code": "PL", "name": "Plateau", "geo_zone": "North Central", "capital": "Jos"},
    {"code": "RI", "name": "Rivers", "geo_zone": "South South", "capital": "Port Harcourt"},
    {"code": "SO", "name": "Sokoto", "geo_zone": "North West", "capital": "Sokoto"},
    {"code": "TA", "name": "Taraba", "geo_zone": "North East", "capital": "Jalingo"},
    {"code": "YO", "name": "Yobe", "geo_zone": "North East", "capital": "Damaturu"},
    {"code": "ZA", "name": "Zamfara", "geo_zone": "North West", "capital": "Gusau"},
]

SAMPLE_LGAS = {
    "LA": ["Agege", "Ajeromi-Ifelodun", "Alimosho", "Amuwo-Odofin", "Apapa", "Badagry", "Epe", "Eti-Osa", "Ibeju-Lekki", "Ifako-Ijaiye", "Ikeja", "Ikorodu", "Kosofe", "Lagos Island", "Lagos Mainland", "Mushin", "Ojo", "Oshodi-Isolo", "Shomolu", "Surulere"],
    "FC": ["Abaji", "Bwari", "Gwagwalada", "Kuje", "Kwali", "Municipal"],
    "KN": ["Ajingi", "Albasu", "Bagwai", "Bebeji", "Bichi", "Bunkure", "Dala", "Dambatta", "Dawakin Kudu", "Dawakin Tofa", "Doguwa", "Fagge", "Gabasawa", "Garko", "Garun Mallam", "Gaya", "Gezawa", "Gwale", "Gwarzo", "Kabo", "Kano Municipal", "Karaye", "Kibiya", "Kiru", "Kumbotso", "Kunchi", "Kura", "Madobi", "Makoda", "Minjibir", "Nassarawa", "Rano", "Rimin Gado", "Rogo", "Shanono", "Sumaila", "Takai", "Tarauni", "Tofa", "Tsanyawa", "Tudun Wada", "Ungogo", "Warawa", "Wudil"],
    "RI": ["Abua/Odual", "Ahoada East", "Ahoada West", "Akuku Toru", "Andoni", "Asari-Toru", "Bonny", "Degema", "Emohua", "Eleme", "Etche", "Gokana", "Ikwerre", "Khana", "Obia/Akpor", "Ogba/Egbema/Ndoni", "Ogu/Bolo", "Okrika", "Omuma", "Opobo/Nkoro", "Oyigbo", "Port Harcourt", "Tai"],
    "OY": ["Afijio", "Akinyele", "Atiba", "Atisbo", "Egbeda", "Ibadan North", "Ibadan North East", "Ibadan North West", "Ibadan South East", "Ibadan South West", "Ibarapa Central", "Ibarapa East", "Ibarapa North", "Ido", "Irepo", "Iseyin", "Itesiwaju", "Iwajowa", "Kajola", "Lagelu", "Ogbomoso North", "Ogbomoso South", "Ogo Oluwa", "Oluyole", "Ona Ara", "Orelope", "Ori Ire", "Oyo East", "Oyo West", "Saki East", "Saki West", "Surulere"],
    "AB": ["Aba North", "Aba South", "Arochukwu", "Bende", "Ikwuano", "Isiala Ngwa North", "Isiala Ngwa South", "Isuikwuato", "Obi Ngwa", "Ohafia", "Osisioma Ngwa", "Ugwunagbo", "Ukwa East", "Ukwa West", "Umuahia North", "Umuahia South", "Umu Nneochi"],
    "KD": ["Birnin Gwari", "Chikun", "Giwa", "Igabi", "Ikara", "Jaba", "Jema'a", "Kachia", "Kaduna North", "Kaduna South", "Kagarko", "Kajuru", "Kaura", "Kauru", "Kubau", "Kudan", "Lere", "Makarfi", "Sabon Gari", "Sanga", "Soba", "Zangon Kataf", "Zaria"],
}

PARTIES = [
    {"code": "APC", "name": "All Progressives Congress", "abbreviation": "APC", "color": "#0066CC"},
    {"code": "PDP", "name": "Peoples Democratic Party", "abbreviation": "PDP", "color": "#CC0000"},
    {"code": "LP", "name": "Labour Party", "abbreviation": "LP", "color": "#009933"},
    {"code": "NNPP", "name": "New Nigeria Peoples Party", "abbreviation": "NNPP", "color": "#FF6600"},
    {"code": "ADC", "name": "African Democratic Congress", "abbreviation": "ADC", "color": "#9933CC"},
    {"code": "SDP", "name": "Social Democratic Party", "abbreviation": "SDP", "color": "#FFD700"},
    {"code": "APGA", "name": "All Progressives Grand Alliance", "abbreviation": "APGA", "color": "#228B22"},
    {"code": "YPP", "name": "Young Progressives Party", "abbreviation": "YPP", "color": "#FF1493"},
]

WARD_NAMES = [
    "Ward I", "Ward II", "Ward III", "Ward IV", "Ward V",
    "Ward VI", "Ward VII", "Ward VIII", "Ward IX", "Ward X",
    "Ward XI", "Ward XII"
]

def seed_database():
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()

    cursor.execute("SELECT COUNT(*) FROM states")
    if cursor.fetchone()[0] > 0:
        conn.close()
        return

    for s in NIGERIA_STATES:
        cursor.execute("INSERT INTO states (code, name, geo_zone, capital) VALUES (?, ?, ?, ?)",
                       (s["code"], s["name"], s["geo_zone"], s["capital"]))

    for p in PARTIES:
        cursor.execute("INSERT INTO parties (code, name, abbreviation, color) VALUES (?, ?, ?, ?)",
                       (p["code"], p["name"], p["abbreviation"], p["color"]))

    admin_hash = pbkdf2_sha256.hash("admin123")
    cursor.execute("INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?, ?, ?, ?, ?)",
                   ("admin", admin_hash, "System Administrator", "admin", "INEC-ADMIN-001"))

    observer_hash = pbkdf2_sha256.hash("observer123")
    cursor.execute("INSERT INTO users (username, password_hash, full_name, role, staff_id) VALUES (?, ?, ?, ?, ?)",
                   ("observer", observer_hash, "Election Observer", "observer", "OBS-001"))

    pu_counter = 0
    for state in NIGERIA_STATES:
        state_code = state["code"]
        lga_names = SAMPLE_LGAS.get(state_code, [f"LGA-{i+1}" for i in range(random.randint(8, 20))])

        for idx, lga_name in enumerate(lga_names):
            lga_code = f"{state_code}-{idx+1:03d}"
            cursor.execute("INSERT INTO lgas (code, name, state_code) VALUES (?, ?, ?)",
                           (lga_code, lga_name, state_code))

            num_wards = random.randint(8, 15)
            for w_idx in range(num_wards):
                ward_code = f"{lga_code}-W{w_idx+1:03d}"
                ward_name = WARD_NAMES[w_idx % len(WARD_NAMES)] if w_idx < len(WARD_NAMES) else f"Ward {w_idx+1}"
                cursor.execute("INSERT INTO wards (code, name, lga_code) VALUES (?, ?, ?)",
                               (ward_code, ward_name, lga_code))

                num_pus = random.randint(3, 8)
                for p_idx in range(num_pus):
                    pu_code = f"{ward_code}-PU{p_idx+1:03d}"
                    pu_name = f"Polling Unit {p_idx+1}"
                    registered = random.randint(200, 1200)
                    lat = 4.0 + random.random() * 10
                    lng = 2.5 + random.random() * 12
                    cursor.execute(
                        "INSERT INTO polling_units (code, name, ward_code, registered_voters, latitude, longitude) VALUES (?, ?, ?, ?, ?, ?)",
                        (pu_code, pu_name, ward_code, registered, round(lat, 6), round(lng, 6)))
                    pu_counter += 1

    officer_hash = pbkdf2_sha256.hash("officer123")
    cursor.execute("INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code, polling_unit_code) VALUES (?, ?, ?, ?, ?, ?, ?)",
                   ("officer1", officer_hash, "Adeola Bakare", "presiding_officer", "INEC-LA-00234", "LA", "LA-001-W001-PU001"))

    cursor.execute("""INSERT INTO elections (title, election_type, election_date, status, description, total_registered_voters)
                      VALUES (?, ?, ?, ?, ?, ?)""",
                   ("2027 Presidential Election", "presidential", "2027-02-28", "active",
                    "General election for the President of the Federal Republic of Nigeria", pu_counter * 500))

    cursor.execute("SELECT id FROM elections LIMIT 1")
    election_id = cursor.fetchone()[0]

    cursor.execute("SELECT code FROM polling_units ORDER BY RANDOM() LIMIT 800")
    sample_pus = [row[0] for row in cursor.fetchall()]

    party_codes = [p["code"] for p in PARTIES]
    for pu_code in sample_pus:
        cursor.execute("SELECT registered_voters FROM polling_units WHERE code=?", (pu_code,))
        registered = cursor.fetchone()[0]
        accredited = int(registered * random.uniform(0.4, 0.85))
        rejected = random.randint(0, int(accredited * 0.05))
        valid_votes = accredited - rejected

        weights = [random.random() for _ in party_codes]
        total_w = sum(weights)
        party_votes = {}
        remaining = valid_votes
        for i, pc in enumerate(party_codes):
            if i == len(party_codes) - 1:
                party_votes[pc] = remaining
            else:
                v = int(valid_votes * weights[i] / total_w)
                party_votes[pc] = v
                remaining -= v

        tb_id = f"TB-{random.randint(100000,999999)}"
        hl_id = f"0x{random.randbytes(16).hex()}"
        ipfs_cid = f"Qm{random.randbytes(22).hex()}"
        ec8a_hash = f"sha256:{hashlib.sha256(pu_code.encode()).hexdigest()}"

        status = random.choices(["finalized", "validated", "pending"], weights=[70, 20, 10])[0]
        tb_status = "POSTED" if status == "finalized" else "PENDING"
        hl_status = "CONFIRMED" if status == "finalized" else "PENDING"

        cursor.execute("""INSERT INTO results (election_id, polling_unit_code, presiding_officer_id,
                          status, total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
                          ec8a_hash, tigerbeetle_transfer_id, hyperledger_tx_id, tigerbeetle_status,
                          hyperledger_status, ipfs_cid)
                          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                       (election_id, pu_code, 3, status, valid_votes, rejected, accredited, accredited,
                        ec8a_hash, tb_id, hl_id, tb_status, hl_status, ipfs_cid))

        result_id = cursor.lastrowid
        for pc, votes in party_votes.items():
            cursor.execute("INSERT INTO result_party_scores (result_id, party_code, votes) VALUES (?, ?, ?)",
                           (result_id, pc, votes))

    prev_hash = "0" * 64
    actions = ["RESULT_SUBMITTED", "RESULT_VALIDATED", "RESULT_FINALIZED", "ELECTION_CREATED", "USER_LOGIN"]
    for i in range(50):
        action = random.choice(actions)
        block_data = f"{prev_hash}{action}{i}"
        block_hash = hashlib.sha256(block_data.encode()).hexdigest()
        cursor.execute("""INSERT INTO audit_log (action, entity_type, entity_id, user_id, details, block_hash, prev_block_hash)
                          VALUES (?, ?, ?, ?, ?, ?, ?)""",
                       (action, "result" if "RESULT" in action else "system",
                        str(random.randint(1, 800)), random.choice([1, 2, 3]),
                        json.dumps({"phase": random.choice(["Pre-Validation", "Edge Validation", "Result Submission", "Finalization"])}),
                        block_hash, prev_hash))
        prev_hash = block_hash

    conn.commit()
    conn.close()
    print(f"Seeded {pu_counter} polling units across 37 states")
