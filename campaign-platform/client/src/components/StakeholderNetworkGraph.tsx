/**
 * StakeholderNetworkGraph — D3.js force-directed influence network
 * Shows relationships and coalition pathways between stakeholder groups.
 * Design: Civic Data Observatory dark theme
 */
import { useEffect, useRef, useMemo } from "react";
import type { Stakeholder } from "./StakeholderTypes";

interface NetworkNode {
  id: string;
  label: string;
  category: string;
  score: number;
  reach: number;
  x?: number;
  y?: number;
  vx?: number;
  vy?: number;
  fx?: number | null;
  fy?: number | null;
}

interface NetworkEdge {
  source: string | NetworkNode;
  target: string | NetworkNode;
  strength: number;
  type: "coalition" | "referral" | "overlap" | "conflict";
}

// Category color map
const CATEGORY_COLORS: Record<string, string> = {
  "Traditional Rulers":    "#f59e0b",
  "Youth Groups":          "#10b981",
  "Women Associations":    "#ec4899",
  "Religious Bodies":      "#8b5cf6",
  "Labour & Workers":      "#ef4444",
  "Farmers & Agriculture": "#84cc16",
  "Professional Bodies":   "#06b6d4",
  "Civil Society":         "#f97316",
  "Diaspora":              "#6366f1",
  "Disability Groups":     "#a78bfa",
  "Ethnic & Regional":     "#fbbf24",
  "Media & Influencers":   "#34d399",
};

// Predefined relationship edges between stakeholder categories
const CATEGORY_EDGES: Array<{ from: string; to: string; strength: number; type: NetworkEdge["type"] }> = [
  { from: "Traditional Rulers",    to: "Youth Groups",          strength: 0.7, type: "referral"  },
  { from: "Traditional Rulers",    to: "Women Associations",    strength: 0.8, type: "coalition" },
  { from: "Traditional Rulers",    to: "Religious Bodies",      strength: 0.9, type: "coalition" },
  { from: "Traditional Rulers",    to: "Ethnic & Regional",     strength: 0.95, type: "overlap"  },
  { from: "Youth Groups",          to: "Labour & Workers",      strength: 0.6, type: "coalition" },
  { from: "Youth Groups",          to: "Professional Bodies",   strength: 0.5, type: "referral"  },
  { from: "Youth Groups",          to: "Media & Influencers",   strength: 0.8, type: "coalition" },
  { from: "Youth Groups",          to: "Diaspora",              strength: 0.7, type: "overlap"   },
  { from: "Women Associations",    to: "Farmers & Agriculture", strength: 0.7, type: "coalition" },
  { from: "Women Associations",    to: "Civil Society",         strength: 0.8, type: "coalition" },
  { from: "Women Associations",    to: "Professional Bodies",   strength: 0.6, type: "referral"  },
  { from: "Religious Bodies",      to: "Civil Society",         strength: 0.7, type: "coalition" },
  { from: "Religious Bodies",      to: "Women Associations",    strength: 0.75, type: "coalition"},
  { from: "Labour & Workers",      to: "Farmers & Agriculture", strength: 0.6, type: "coalition" },
  { from: "Labour & Workers",      to: "Civil Society",         strength: 0.7, type: "coalition" },
  { from: "Professional Bodies",   to: "Civil Society",         strength: 0.8, type: "coalition" },
  { from: "Professional Bodies",   to: "Media & Influencers",   strength: 0.6, type: "referral"  },
  { from: "Civil Society",         to: "Diaspora",              strength: 0.5, type: "referral"  },
  { from: "Civil Society",         to: "Disability Groups",     strength: 0.7, type: "coalition" },
  { from: "Ethnic & Regional",     to: "Religious Bodies",      strength: 0.6, type: "overlap"   },
  { from: "Ethnic & Regional",     to: "Civil Society",         strength: 0.5, type: "referral"  },
  { from: "Diaspora",              to: "Media & Influencers",   strength: 0.8, type: "coalition" },
  { from: "Disability Groups",     to: "Civil Society",         strength: 0.8, type: "coalition" },
];

const EDGE_COLORS: Record<NetworkEdge["type"], string> = {
  coalition: "rgba(16, 185, 129, 0.5)",
  referral:  "rgba(99, 102, 241, 0.4)",
  overlap:   "rgba(245, 158, 11, 0.4)",
  conflict:  "rgba(239, 68, 68, 0.4)",
};

interface Props {
  stakeholders: Stakeholder[];
}

export default function StakeholderNetworkGraph({ stakeholders }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);

  // Build nodes from unique categories present in the stakeholder list
  const { nodes, edges } = useMemo(() => {
    const categoryMap = new Map<string, { score: number; reach: number; count: number; label: string }>();

    stakeholders.forEach(s => {
      const cat = s.category;
      const existing = categoryMap.get(cat);
      if (existing) {
        existing.score += s.priority;
        existing.reach += (s.estimated_voter_reach ?? 0);
        existing.count += 1;
      } else {
        categoryMap.set(cat, {
          score: s.priority,
          reach: s.estimated_voter_reach ?? 0,
          count: 1,
          label: s.name,
        });
      }
    });

    const nodes: NetworkNode[] = Array.from(categoryMap.entries()).map(([cat, data]) => ({
      id: cat,
      label: cat,
      category: cat,
      score: data.score / data.count,
      reach: data.reach,
    }));

    const presentCategories = new Set(nodes.map(n => n.id));
    const edges: NetworkEdge[] = CATEGORY_EDGES
      .filter(e => presentCategories.has(e.from) && presentCategories.has(e.to))
      .map(e => ({ source: e.from, target: e.to, strength: e.strength, type: e.type }));

    return { nodes, edges };
  }, [stakeholders]);

  useEffect(() => {
    if (!svgRef.current || nodes.length === 0) return;

    const svg = svgRef.current;
    const W = svg.clientWidth || 700;
    const H = svg.clientHeight || 500;

    // Clear previous render
    while (svg.firstChild) svg.removeChild(svg.firstChild);

    // --- Simple force simulation (no d3 dependency — pure JS) ---
    // Clone nodes with initial positions
    const simNodes: NetworkNode[] = nodes.map((n, i) => ({
      ...n,
      x: W / 2 + Math.cos((i / nodes.length) * 2 * Math.PI) * 180,
      y: H / 2 + Math.sin((i / nodes.length) * 2 * Math.PI) * 180,
      vx: 0, vy: 0,
    }));

    const nodeById = new Map(simNodes.map(n => [n.id, n]));

    // Resolve edge source/target to node objects
    const simEdges = edges.map(e => ({
      ...e,
      source: nodeById.get(e.source as string)!,
      target: nodeById.get(e.target as string)!,
    })).filter(e => e.source && e.target);

    // Run force simulation
    const ALPHA = 0.3;
    const REPULSION = 3500;
    const LINK_DIST = 140;
    const CENTER_STRENGTH = 0.05;

    for (let iter = 0; iter < 300; iter++) {
      const alpha = ALPHA * Math.pow(0.99, iter);

      // Repulsion between all nodes
      for (let i = 0; i < simNodes.length; i++) {
        for (let j = i + 1; j < simNodes.length; j++) {
          const a = simNodes[i], b = simNodes[j];
          const dx = (b.x! - a.x!) || 0.01;
          const dy = (b.y! - a.y!) || 0.01;
          const dist2 = dx * dx + dy * dy;
          const force = REPULSION / dist2;
          a.vx! -= force * dx * alpha;
          a.vy! -= force * dy * alpha;
          b.vx! += force * dx * alpha;
          b.vy! += force * dy * alpha;
        }
      }

      // Link attraction
      simEdges.forEach(e => {
        const s = e.source as NetworkNode, t = e.target as NetworkNode;
        const dx = t.x! - s.x!;
        const dy = t.y! - s.y!;
        const dist = Math.sqrt(dx * dx + dy * dy) || 1;
        const force = (dist - LINK_DIST) * e.strength * alpha * 0.5;
        s.vx! += (dx / dist) * force;
        s.vy! += (dy / dist) * force;
        t.vx! -= (dx / dist) * force;
        t.vy! -= (dy / dist) * force;
      });

      // Center gravity
      simNodes.forEach(n => {
        n.vx! += (W / 2 - n.x!) * CENTER_STRENGTH * alpha;
        n.vy! += (H / 2 - n.y!) * CENTER_STRENGTH * alpha;
        n.x! += n.vx!;
        n.y! += n.vy!;
        n.vx! *= 0.6;
        n.vy! *= 0.6;
        // Clamp to bounds
        n.x! = Math.max(60, Math.min(W - 60, n.x!));
        n.y! = Math.max(40, Math.min(H - 40, n.y!));
      });
    }

    const ns = "http://www.w3.org/2000/svg";

    // Defs: arrowheads
    const defs = document.createElementNS(ns, "defs");
    (["coalition", "referral", "overlap"] as const).forEach(type => {
      const marker = document.createElementNS(ns, "marker");
      marker.setAttribute("id", `arrow-${type}`);
      marker.setAttribute("markerWidth", "8");
      marker.setAttribute("markerHeight", "8");
      marker.setAttribute("refX", "6");
      marker.setAttribute("refY", "3");
      marker.setAttribute("orient", "auto");
      const path = document.createElementNS(ns, "path");
      path.setAttribute("d", "M0,0 L0,6 L8,3 z");
      path.setAttribute("fill", EDGE_COLORS[type]);
      marker.appendChild(path);
      defs.appendChild(marker);
    });
    svg.appendChild(defs);

    // Draw edges
    simEdges.forEach(e => {
      const s = e.source as NetworkNode, t = e.target as NetworkNode;
      const line = document.createElementNS(ns, "line");
      line.setAttribute("x1", String(s.x!));
      line.setAttribute("y1", String(s.y!));
      line.setAttribute("x2", String(t.x!));
      line.setAttribute("y2", String(t.y!));
      line.setAttribute("stroke", EDGE_COLORS[e.type]);
      line.setAttribute("stroke-width", String(1 + e.strength * 2));
      line.setAttribute("marker-end", `url(#arrow-${e.type})`);
      svg.appendChild(line);
    });

    // Draw nodes
    simNodes.forEach(n => {
      const color = CATEGORY_COLORS[n.category] ?? "#94a3b8";
      const radius = 18 + (n.score / 10) * 8;

      // Glow circle
      const glow = document.createElementNS(ns, "circle");
      glow.setAttribute("cx", String(n.x!));
      glow.setAttribute("cy", String(n.y!));
      glow.setAttribute("r", String(radius + 6));
      glow.setAttribute("fill", color);
      glow.setAttribute("opacity", "0.12");
      svg.appendChild(glow);

      // Main circle
      const circle = document.createElementNS(ns, "circle");
      circle.setAttribute("cx", String(n.x!));
      circle.setAttribute("cy", String(n.y!));
      circle.setAttribute("r", String(radius));
      circle.setAttribute("fill", color);
      circle.setAttribute("fill-opacity", "0.85");
      circle.setAttribute("stroke", color);
      circle.setAttribute("stroke-width", "2");
      circle.style.cursor = "pointer";

      // Tooltip on hover
      circle.addEventListener("mouseenter", (ev) => {
        const tip = tooltipRef.current;
        if (!tip) return;
        const reach = n.reach >= 1000000
          ? `${(n.reach / 1000000).toFixed(1)}M`
          : `${(n.reach / 1000).toFixed(0)}K`;
        tip.innerHTML = `<div class="font-bold text-sm mb-1">${n.label}</div><div class="text-xs opacity-70">Priority Score: ${n.score.toFixed(1)}/10</div><div class="text-xs opacity-70">Est. Reach: ~${reach} voters</div>`;
        tip.style.display = "block";
        tip.style.left = `${(ev as MouseEvent).offsetX + 12}px`;
        tip.style.top = `${(ev as MouseEvent).offsetY - 10}px`;
      });
      circle.addEventListener("mouseleave", () => {
        if (tooltipRef.current) tooltipRef.current.style.display = "none";
      });
      svg.appendChild(circle);

      // Label
      const words = n.label.split(" ");
      const text = document.createElementNS(ns, "text");
      text.setAttribute("x", String(n.x!));
      text.setAttribute("y", String(n.y! + radius + 14));
      text.setAttribute("text-anchor", "middle");
      text.setAttribute("fill", "oklch(0.82 0.005 240)");
      text.setAttribute("font-size", "9");
      text.setAttribute("font-family", "monospace");
      text.style.pointerEvents = "none";
      words.forEach((word, i) => {
        const tspan = document.createElementNS(ns, "tspan");
        tspan.setAttribute("x", String(n.x!));
        tspan.setAttribute("dy", i === 0 ? "0" : "11");
        tspan.textContent = word;
        text.appendChild(tspan);
      });
      svg.appendChild(text);
    });
  }, [nodes, edges]);

  if (stakeholders.length === 0) {
    return (
      <div className="flex items-center justify-center h-64 text-sm" style={{ color: "oklch(0.45 0.01 240)" }}>
        Generate a stakeholder plan first to view the influence network.
      </div>
    );
  }

  return (
    <div className="relative w-full" style={{ background: "oklch(0.10 0.008 240)", border: "1px solid oklch(0.22 0.01 240)", borderRadius: "0.5rem" }}>
      {/* Legend */}
      <div className="flex flex-wrap gap-4 px-4 pt-3 pb-2 border-b" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
        <span className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.55 0.01 240)" }}>EDGE TYPES:</span>
        {(["coalition", "referral", "overlap"] as const).map(type => (
          <span key={type} className="flex items-center gap-1.5 text-xs" style={{ color: "oklch(0.65 0.01 240)" }}>
            <span className="inline-block w-6 h-0.5 rounded" style={{ background: EDGE_COLORS[type] }} />
            {type.charAt(0).toUpperCase() + type.slice(1)}
          </span>
        ))}
        <span className="ml-auto text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
          {nodes.length} groups · {edges.length} relationships
        </span>
      </div>

      {/* SVG canvas */}
      <div className="relative">
        <svg
          ref={svgRef}
          className="w-full"
          style={{ height: "480px" }}
        />
        {/* Tooltip */}
        <div
          ref={tooltipRef}
          className="absolute hidden px-3 py-2 rounded text-xs pointer-events-none z-10"
          style={{
            background: "oklch(0.18 0.01 240)",
            border: "1px solid oklch(0.32 0.01 240)",
            color: "oklch(0.88 0.005 240)",
            maxWidth: "180px",
          }}
        />
      </div>

      {/* Node color legend */}
      <div className="flex flex-wrap gap-2 px-4 py-3 border-t" style={{ borderColor: "oklch(0.22 0.01 240)" }}>
        {nodes.map(n => (
          <span key={n.id} className="flex items-center gap-1 text-xs" style={{ color: "oklch(0.65 0.01 240)" }}>
            <span className="inline-block w-2.5 h-2.5 rounded-full" style={{ background: CATEGORY_COLORS[n.category] ?? "#94a3b8" }} />
            {n.label}
          </span>
        ))}
      </div>
    </div>
  );
}
