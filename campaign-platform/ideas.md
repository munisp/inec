# INEC Digital Twin Simulation Engine — Design Brainstorm

## Three Stylistic Approaches

**Approach A — Mission Control (Probability: 0.08)**
Dark aerospace dashboard aesthetic. Monospace data readouts, glowing green-on-dark charts, grid overlays. Feels like a NASA operations center.

**Approach B — Civic Data Observatory (Probability: 0.06)**
Clean, authoritative data journalism aesthetic. Off-white backgrounds, deep burgundy (#4A1525) and forest green (#008751) as the Nigerian national palette, bold serif display headers, generous whitespace. Feels like an Economist or FT data visualization.

**Approach C — Tactical War Room (Probability: 0.05)**
Military-grade dark UI. Slate-black backgrounds, amber alert colors, sharp geometric shapes, no decorative elements. Feels like a NORAD command center.

---

## Chosen Approach: B — Civic Data Observatory

**Design Movement:** Data Journalism meets Governmental Authority

**Core Principles:**
1. Data is the hero — every element serves to communicate information clearly
2. Nigerian national identity — the burgundy/green palette is non-negotiable
3. Authority through restraint — no decorative elements that don't carry information
4. Interactivity as education — controls teach the user how the simulation works

**Color Philosophy:**
- Background: `#F5F0EB` (warm off-white — feels like quality newsprint, not sterile white)
- Primary: `#4A1525` (deep Nigerian flag burgundy — authority and gravitas)
- Accent: `#008751` (Nigerian flag green — progress and validation)
- Data: `#1A3A5C` (deep navy — charts and data visualization)
- Alert: `#C0392B` (crisis red — adversarial scenarios)
- Text: `#1C1C1C` (near-black — maximum readability)

**Layout Paradigm:**
Asymmetric split-screen. Left sidebar (30%) for controls and scenario selection. Right main area (70%) for live simulation output and charts. Top bar for platform identity and key metrics. No centered hero sections.

**Signature Elements:**
1. Thin 2px horizontal rules in `#008751` to separate data sections (no full borders)
2. Monospace numbers for all data readouts (Roboto Mono)
3. Bold uppercase section labels in `#4A1525` with letter-spacing

**Typography System:**
- Display: `Playfair Display` (serif) — for the platform title and section headings
- Body/UI: `Inter` — for controls, labels, and body text
- Data: `Roboto Mono` — for all numerical outputs and simulation values

**Brand Essence:** The definitive intelligence platform for Nigeria's democratic process. For electoral administrators and campaign strategists who need certainty, not guesswork.

**Brand Voice:** Authoritative, precise, calm. Headlines are declarative statements of fact. CTAs are direct commands. Example: "Run Simulation" not "Start Simulating". "View Scenario Output" not "See What Happens".

**Signature Brand Color:** `#4A1525` — deep Nigerian burgundy.

## Style Decisions
- Use `Playfair Display` for all H1/H2 headings to establish authority
- Use `Roboto Mono` for all numerical data outputs
- Simulation controls use `#4A1525` as the primary button color
- Charts use a progression from `#008751` (low) to `#4A1525` (high) to `#C0392B` (crisis)
- All section separators are `2px solid #008751`, never full card borders
