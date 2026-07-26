-- 000020: Database-enforced immutability for election evidence and approved provenance.
-- The application builds signed hash chains; these guards ensure a privileged SQL client
-- cannot silently rewrite the chain, its artifacts, or a completed assessment.

CREATE OR REPLACE FUNCTION prevent_election_evidence_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% on % is prohibited: election evidence is immutable', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_result_evidence_events_immutable ON result_evidence_events;
CREATE TRIGGER trg_result_evidence_events_immutable
    BEFORE UPDATE OR DELETE ON result_evidence_events
    FOR EACH ROW EXECUTE FUNCTION prevent_election_evidence_mutation();

DROP TRIGGER IF EXISTS trg_evidence_artifacts_immutable ON evidence_artifacts;
CREATE TRIGGER trg_evidence_artifacts_immutable
    BEFORE UPDATE OR DELETE ON evidence_artifacts
    FOR EACH ROW EXECUTE FUNCTION prevent_election_evidence_mutation();

DROP TRIGGER IF EXISTS trg_document_integrity_assessments_immutable ON document_integrity_assessments;
CREATE TRIGGER trg_document_integrity_assessments_immutable
    BEFORE UPDATE OR DELETE ON document_integrity_assessments
    FOR EACH ROW EXECUTE FUNCTION prevent_election_evidence_mutation();

CREATE OR REPLACE FUNCTION enforce_policy_version_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'election policy versions cannot be deleted; revoke or supersede them instead'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'approved' THEN
        IF NEW.election_id <> OLD.election_id
           OR NEW.version <> OLD.version
           OR NEW.legal_basis <> OLD.legal_basis
           OR NEW.rules_json <> OLD.rules_json
           OR NEW.rules_sha256 <> OLD.rules_sha256
           OR NEW.approved_by IS DISTINCT FROM OLD.approved_by
           OR NEW.approved_at IS DISTINCT FROM OLD.approved_at
           OR NEW.effective_from <> OLD.effective_from
           OR NEW.status NOT IN ('approved', 'superseded', 'revoked') THEN
            RAISE EXCEPTION 'approved policy versions are immutable; create a new version instead'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_policy_version_transition ON election_policy_versions;
CREATE TRIGGER trg_policy_version_transition
    BEFORE UPDATE OR DELETE ON election_policy_versions
    FOR EACH ROW EXECUTE FUNCTION enforce_policy_version_transition();

CREATE OR REPLACE FUNCTION enforce_material_manifest_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'election material manifests cannot be deleted; revoke or supersede them instead'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'approved' THEN
        IF NEW.election_id <> OLD.election_id
           OR NEW.policy_version_id <> OLD.policy_version_id
           OR NEW.material_type <> OLD.material_type
           OR NEW.version <> OLD.version
           OR NEW.manifest_sha256 <> OLD.manifest_sha256
           OR NEW.artifact_id IS DISTINCT FROM OLD.artifact_id
           OR NEW.approved_by IS DISTINCT FROM OLD.approved_by
           OR NEW.approved_at IS DISTINCT FROM OLD.approved_at
           OR NEW.effective_from <> OLD.effective_from
           OR NEW.status NOT IN ('approved', 'superseded', 'revoked') THEN
            RAISE EXCEPTION 'approved material manifests are immutable; create a new version instead'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_material_manifest_transition ON election_material_manifests;
CREATE TRIGGER trg_material_manifest_transition
    BEFORE UPDATE OR DELETE ON election_material_manifests
    FOR EACH ROW EXECUTE FUNCTION enforce_material_manifest_transition();

CREATE OR REPLACE FUNCTION enforce_collation_bundle_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'collation evidence bundles cannot be deleted; supersede or block them instead'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    IF OLD.status = 'published' THEN
        IF NEW.election_id <> OLD.election_id
           OR NEW.level <> OLD.level
           OR NEW.area_code <> OLD.area_code
           OR NEW.bundle_version <> OLD.bundle_version
           OR NEW.child_results_sha256 <> OLD.child_results_sha256
           OR NEW.aggregate_sha256 <> OLD.aggregate_sha256
           OR NEW.event_root_sha256 IS DISTINCT FROM OLD.event_root_sha256
           OR NEW.policy_version_id IS DISTINCT FROM OLD.policy_version_id
           OR NEW.artifact_id IS DISTINCT FROM OLD.artifact_id
           OR NEW.created_by IS DISTINCT FROM OLD.created_by
           OR NEW.created_at <> OLD.created_at
           OR NEW.status NOT IN ('published', 'superseded', 'blocked') THEN
            RAISE EXCEPTION 'published collation bundles are immutable; publish a new bundle version instead'
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_collation_bundle_transition ON collation_evidence_bundles;
CREATE TRIGGER trg_collation_bundle_transition
    BEFORE UPDATE OR DELETE ON collation_evidence_bundles
    FOR EACH ROW EXECUTE FUNCTION enforce_collation_bundle_transition();
