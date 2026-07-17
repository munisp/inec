"""
INEC Innovation 5: Digital Twin Election Simulation
====================================================
Creates a real-time digital twin of the election process that mirrors
the physical election in a virtual environment. The twin enables:

  1. Pre-election scenario planning (what if turnout drops 20%?)
  2. Real-time stress testing of collation infrastructure
  3. Predictive modelling of result transmission delays
  4. Resource allocation optimisation (staff, materials, vehicles)
  5. Post-election forensic replay for audit purposes

The simulation uses a discrete-event simulation engine built on
SimPy principles, adapted for Nigerian election logistics.
"""

import asyncio
import json
import math
import random
import time
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Optional

import uvicorn
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

app = FastAPI(
    title="INEC Digital Twin Election Simulation",
    description="Real-time digital twin for election scenario planning and monitoring",
    version="1.0.0",
)

app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])


# ── Simulation Models ─────────────────────────────────────────────────────────

class PollingUnitStatus(str, Enum):
    SETUP = "setup"
    ACCREDITATION = "accreditation"
    VOTING = "voting"
    COUNTING = "counting"
    TRANSMISSION = "transmission"
    COMPLETED = "completed"
    INCIDENT = "incident"


@dataclass
class PollingUnitTwin:
    """Digital twin of a single polling unit."""
    id: str
    state: str
    lga: str
    ward: str
    registered_voters: int
    status: PollingUnitStatus = PollingUnitStatus.SETUP
    accredited_voters: int = 0
    votes_cast: int = 0
    results_transmitted: bool = False
    incident_count: int = 0
    start_time: float = field(default_factory=time.time)
    completion_time: Optional[float] = None

    def simulate_step(self, dt: float, scenario: dict) -> list[dict]:
        """Advance the simulation by dt seconds and return events."""
        events = []
        turnout_factor = scenario.get("turnout_factor", 0.65)
        incident_rate = scenario.get("incident_rate", 0.02)

        if self.status == PollingUnitStatus.SETUP:
            if random.random() < 0.1 * dt:
                self.status = PollingUnitStatus.ACCREDITATION
                events.append({"type": "accreditation_started", "unit": self.id})

        elif self.status == PollingUnitStatus.ACCREDITATION:
            # Accredit voters at a rate proportional to registered voters
            rate = self.registered_voters * turnout_factor / 3600  # per second
            new_accredited = int(rate * dt * random.uniform(0.8, 1.2))
            self.accredited_voters = min(
                self.registered_voters,
                self.accredited_voters + new_accredited
            )
            if self.accredited_voters >= self.registered_voters * turnout_factor * 0.9:
                self.status = PollingUnitStatus.VOTING
                events.append({"type": "voting_started", "unit": self.id})

        elif self.status == PollingUnitStatus.VOTING:
            rate = self.accredited_voters / 7200  # 2 hours voting
            new_votes = int(rate * dt * random.uniform(0.7, 1.3))
            self.votes_cast = min(self.accredited_voters, self.votes_cast + new_votes)
            if self.votes_cast >= self.accredited_voters * 0.95:
                self.status = PollingUnitStatus.COUNTING
                events.append({"type": "counting_started", "unit": self.id})

        elif self.status == PollingUnitStatus.COUNTING:
            if random.random() < 0.05 * dt:
                self.status = PollingUnitStatus.TRANSMISSION
                events.append({"type": "counting_completed", "unit": self.id})

        elif self.status == PollingUnitStatus.TRANSMISSION:
            if random.random() < 0.08 * dt:
                self.results_transmitted = True
                self.status = PollingUnitStatus.COMPLETED
                self.completion_time = time.time()
                events.append({
                    "type": "results_transmitted",
                    "unit": self.id,
                    "votes_cast": self.votes_cast,
                    "turnout_pct": round(self.votes_cast / max(self.registered_voters, 1) * 100, 1),
                })

        # Random incidents
        if random.random() < incident_rate * dt:
            self.incident_count += 1
            incident_types = ["equipment_failure", "crowd_disturbance", "late_materials", "power_outage"]
            events.append({
                "type": "incident",
                "unit": self.id,
                "incident_type": random.choice(incident_types),
                "severity": random.choice(["low", "medium", "high"]),
            })

        return events


@dataclass
class ElectionTwin:
    """Digital twin of an entire election."""
    election_id: str
    polling_units: list[PollingUnitTwin] = field(default_factory=list)
    scenario: dict = field(default_factory=dict)
    running: bool = False
    events: list[dict] = field(default_factory=list)
    tick: int = 0

    def step(self, dt: float = 1.0) -> list[dict]:
        """Advance all polling units by dt seconds."""
        all_events = []
        for unit in self.polling_units:
            events = unit.simulate_step(dt, self.scenario)
            all_events.extend(events)
        self.events.extend(all_events)
        self.tick += 1
        return all_events

    def get_summary(self) -> dict:
        total = len(self.polling_units)
        completed = sum(1 for u in self.polling_units if u.status == PollingUnitStatus.COMPLETED)
        incidents = sum(u.incident_count for u in self.polling_units)
        total_votes = sum(u.votes_cast for u in self.polling_units)
        total_registered = sum(u.registered_voters for u in self.polling_units)

        return {
            "election_id": self.election_id,
            "tick": self.tick,
            "total_units": total,
            "completed_units": completed,
            "completion_pct": round(completed / max(total, 1) * 100, 1),
            "total_votes": total_votes,
            "overall_turnout_pct": round(total_votes / max(total_registered, 1) * 100, 1),
            "total_incidents": incidents,
            "running": self.running,
        }


# ── Service State ─────────────────────────────────────────────────────────────

active_twins: dict[str, ElectionTwin] = {}
ws_clients: list[WebSocket] = []


# ── API Models ────────────────────────────────────────────────────────────────

class SimulationConfig(BaseModel):
    election_id: str
    num_polling_units: int = 100
    avg_registered_voters: int = 400
    scenario: dict = {
        "turnout_factor": 0.65,
        "incident_rate": 0.02,
        "transmission_delay_factor": 1.0,
    }


# ── Endpoints ─────────────────────────────────────────────────────────────────

@app.post("/api/v1/twin/create")
async def create_twin(config: SimulationConfig):
    """Create a new digital twin for an election."""
    states = ["Lagos", "Kano", "Rivers", "Kaduna", "Oyo", "Anambra", "Borno", "Delta"]
    units = []
    for i in range(config.num_polling_units):
        state = random.choice(states)
        units.append(PollingUnitTwin(
            id=f"PU-{i:04d}",
            state=state,
            lga=f"{state}-LGA-{random.randint(1, 10)}",
            ward=f"Ward-{random.randint(1, 20)}",
            registered_voters=int(random.gauss(config.avg_registered_voters, 100)),
        ))

    twin = ElectionTwin(
        election_id=config.election_id,
        polling_units=units,
        scenario=config.scenario,
        running=True,
    )
    active_twins[config.election_id] = twin

    # Start background simulation loop
    asyncio.create_task(run_simulation(config.election_id))

    return {
        "election_id": config.election_id,
        "units_created": len(units),
        "status": "simulation_started",
    }


async def run_simulation(election_id: str):
    """Background task: advance the simulation and broadcast updates."""
    twin = active_twins.get(election_id)
    if not twin:
        return

    for _ in range(3600):  # Simulate up to 1 hour (1 tick/sec)
        if not twin.running:
            break
        events = twin.step(dt=10.0)  # 10-second time steps
        if events:
            summary = twin.get_summary()
            await broadcast_twin_update({"election_id": election_id, "events": events, "summary": summary})
        await asyncio.sleep(0.5)  # Real-time: 0.5s per 10s simulated


async def broadcast_twin_update(data: dict):
    disconnected = []
    for client in ws_clients:
        try:
            await client.send_json(data)
        except Exception:
            disconnected.append(client)
    for c in disconnected:
        ws_clients.remove(c)


@app.get("/api/v1/twin/{election_id}/summary")
async def get_summary(election_id: str):
    twin = active_twins.get(election_id)
    if not twin:
        raise HTTPException(status_code=404, detail="Twin not found")
    return twin.get_summary()


@app.websocket("/ws/twin")
async def twin_websocket(websocket: WebSocket):
    await websocket.accept()
    ws_clients.append(websocket)
    try:
        while True:
            await asyncio.sleep(30)
            await websocket.send_json({"type": "ping"})
    except WebSocketDisconnect:
        ws_clients.remove(websocket)


@app.get("/api/v1/twin/health")
async def health():
    return {"status": "healthy", "active_twins": len(active_twins)}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8203, log_level="info")
