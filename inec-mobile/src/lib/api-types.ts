/**
 * API entity types with no runtime dependencies (safe to import from pure
 * logic modules and tests). api.ts re-exports these.
 */
export interface Election {
  id: number;
  name: string;
  type: string;
  date: string;
  status: string;
  total_polling_units: number;
  results_submitted: number;
  registered_voters: number;
}
