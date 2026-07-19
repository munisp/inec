// Shared types for the Stakeholder Engagement Engine
export interface Stakeholder {
  id: string;
  name: string;
  category: string;
  subcategory: string;
  reach_pct: number;
  priority: number;
  engagement_method: string[];
  talking_points: string[];
  cultural_protocol: string;
  office_relevance: Record<string, number>;
  best_engagement_time: string;
  key_ask: string;
  estimated_voter_reach?: number;
  relevance_score?: number;
}

export interface LGALeader {
  id: string;
  lga: string;
  state: string;
  name: string;
  role: string;
  category: string;
  influence_level: "High" | "Medium" | "Low";
  contact_method: string;
  notes: string;
}

export interface CRMContact {
  id: string;
  stakeholderId: string;
  stakeholderName: string;
  contactName: string;
  role: string;
  phone: string;
  email: string;
  status: "Not Started" | "Contacted" | "Meeting Scheduled" | "Met" | "Endorsed" | "Declined";
  lastContact: string;
  nextAction: string;
  notes: string;
  createdAt: string;
  verified?: boolean;
}
