# Project TODO

## Phase 11 — Auth Gate, Export, Notifications

- [x] User authentication gate: switch all 20 module routers from publicProcedure to protectedProcedure
- [x] Per-user data isolation: filter all DB queries by ctx.user.id via candidate_profiles.user_id FK
- [x] Login wall: redirect unauthenticated users to Manus OAuth login from hub page
- [x] PDF export for Budget Planner (client-side jsPDF)
- [x] CSV export for Budget Planner
- [x] PDF export for Fundraising Tracker (client-side jsPDF)
- [x] CSV export for Fundraising Tracker
- [x] PDF export for Post-Election Analytics (client-side jsPDF)
- [x] CSV export for Post-Election Analytics
- [x] Heartbeat cron: hourly check for deadlines within 48 hours (task_uid: CtKwktDiMzJRWiuqUmtDYF)
- [x] /api/scheduled/deadline-check handler: query critical timeline_events, send notifyOwner alerts
- [x] Mount /api/scheduled/deadline-check in server/_core/index.ts
- [x] getAllProfiles db helper added to server/db.ts
- [x] scheduled-tasks.json created to record heartbeat task UIDs
- [x] TypeScript: 0 errors confirmed

## Phase 12 — AI Drafting, Multi-User Roles, Mobile Responsive

- [x] AI press release drafting: tRPC pressRelease.aiDraft procedure using invokeLLM
- [x] AI draft button in PressReleaseGenerator UI with loading state and Streamdown rendering
- [x] campaign_members table in schema (profileId, userId, role: owner/manager/viewer)
- [x] tRPC mutations: inviteMember, updateMemberRole, removeMember, listMembers
- [x] Team Management page (CampaignTeam.tsx) with invite form and member list
- [x] /team route wired in App.tsx and hub card added to Home.tsx
- [x] Role-gated UI: viewers cannot add/delete/edit records (show disabled state)
- [x] Mobile-responsive hub page (Home.tsx): single-column on mobile, 2-col on sm, 4-col on lg
- [x] Mobile-responsive War Room: stacked layout on mobile
- [x] Mobile-responsive Campaign Timeline: full-width cards on mobile
- [x] Mobile-responsive Legal Compliance: stacked table on mobile
- [x] Mobile nav: hamburger menu or bottom nav for small screens

## Phase 12 — Gap Items

- [x] Role-aware CRUD guards: load current member role in CandidateProfileContext, disable/hide add/edit/delete buttons for viewers on all CRUD pages
- [x] LegalCompliance mobile table: already uses card-based layout (no table), confirmed mobile-friendly

## Phase 13 — AI Debate Prep, Public Petition, Dashboard KPIs

- [x] AI debate prep: tRPC debateCoach.aiPrep procedure using invokeLLM + opponent positions from OppositionResearch
  - [x] AI debate prep: tRPC debateCoach.aiPrep procedure using invokeLLM + opponent positions from OppositionResearch
  - [x] DebateCoach page: AI Prep panel with opponent selector, talking points generation, rebuttal suggestions
  - [x] Public petition signing: publicProcedure petition.getPublic + petition.sign (no auth required)
  - [x] /sign/:petitionId route in App.tsx with PetitionSignPage component (unauthenticated)
  - [x] Shareable petition link shown in PetitionDrive page
  - [x] /dashboard route: live KPI overview page with cards from all 21 modules
  - [x] Dashboard KPIs: total volunteers, compliance score, fundraising total, days to election, petition signatures, team members
  - [x] Dashboard charts: timeline progress bar, compliance gauge, fundraising vs budget donut
  - [x] TypeScript: 0 errors

## Phase 14 — AI Manifesto, Volunteer Tasks, Simulation Persistence

- [x] AI manifesto drafting: tRPC manifesto.aiDraft procedure using invokeLLM
- [x] ManifestoBuilder UI: AI Draft button per policy section with Streamdown streaming output
- [x] volunteer_tasks table: id, profileId, volunteerId, title, description, taskType, status, dueDate
- [x] tRPC volunteer task CRUD: list, create, updateStatus, delete
- [x] VolunteerPortal UI: task assignment panel — assign tasks to volunteers, track completion
- [x] simulation_runs table: id, profileId, scenario, parameters (jsonb), results (jsonb), createdAt
- [x] tRPC simulation.save and simulation.list procedures
- [x] Home.tsx simulation engine: Save Run button + Run History panel showing past runs
- [x] TypeScript: 0 errors

## Phase 15 — Simulation Enhancements

- [x] AI scenario narrative: tRPC simulation.narrative procedure using invokeLLM to generate plain-English summary after each run
- [x] Home.tsx: AI narrative panel shown below KPI grid after simulation completes
- [x] Simulation comparison view: select two saved runs in Run History tab and show side-by-side KPI diff
- [x] Simulation history PDF export: jsPDF table of all saved runs
- [x] Simulation history CSV export: download all saved runs as CSV
- [x] TypeScript: 0 errors

## Phase 16 — Simulation Deep Features + GitHub Sync

- [ ] DB schema: add ai_narrative TEXT column to simulation_runs table
- [ ] db.ts: update saveSimulationRun helper to accept and store aiNarrative
- [ ] routers.ts: pass narrative to save mutation after narrative is generated
- [ ] Home.tsx: display stored narrative in Run History cards
- [ ] Sensitivity heatmap: sweep weatherSeverity vs securityThreat grid (11x11) and render D3 heatmap in new chart tab
- [ ] Share report: button generates a formatted plain-text/WhatsApp-ready summary of current run KPIs + AI narrative, copies to clipboard
- [ ] GitHub: merge feat/production-readiness-audit into main (27 unmerged commits)
- [ ] TypeScript: 0 errors
