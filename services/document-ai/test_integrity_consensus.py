"""Focused regression coverage for the election-document evidence consensus path."""

import importlib.util
import io
from pathlib import Path

from fastapi import HTTPException
from PIL import Image, ImageDraw

MODULE_PATH = Path(__file__).with_name("main.py")
SPEC = importlib.util.spec_from_file_location("document_ai_main", MODULE_PATH)
assert SPEC and SPEC.loader
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def document_image_bytes() -> bytes:
    image = Image.new("RGB", (900, 900), "white")
    draw = ImageDraw.Draw(image)
    for position in range(40, 860, 80):
        draw.rectangle((position, 40, position + 25, 860), fill="black")
        draw.line((40, position, 860, position), fill="black", width=5)
    output = io.BytesIO()
    image.save(output, format="JPEG", quality=95)
    return output.getvalue()


def valid_ocr() -> object:
    return module.EC8AExtraction(
        serial_number="EC8A/NG/2026/0001",
        polling_unit_code="PU-001",
        polling_unit_name=None,
        ward=None,
        lga=None,
        state=None,
        election_type=None,
        party_results=[{"party_code": "AAA", "votes": 100, "confidence": 0.95}],
        total_valid_votes=100,
        total_rejected_votes=0,
        total_votes_cast=100,
        accredited_voters=100,
        registered_voters=120,
        presiding_officer_name=None,
        raw_ocr_text="EC8A PU-001 AAA 100 Total Valid 100 Accredited 100",
        confidence_score=0.95,
        extraction_warnings=[],
    )


def valid_vlm(tampering: bool = False) -> object:
    return module.VLMResult(
        is_valid_ec8a=True,
        tampering_detected=tampering,
        tampering_confidence=0.95 if tampering else 0.02,
        tampering_indicators=["overwritten digit"] if tampering else [],
        document_quality="good",
        orientation_correct=True,
        completeness_score=0.95,
        analysis_summary="Structured assessment only.",
    )


def valid_docling() -> object:
    return module.DocLingResult(
        tables=[
            module.DocumentTable(
                headers=["party", "votes"],
                rows=[{"party": "AAA", "votes": 100}],
                confidence=0.92,
            )
        ],
        metadata={"title": "EC8A"},
        page_count=1,
        extraction_method="docling",
    )


def run() -> None:
    original_policy = module.DOCUMENT_INTEGRITY_POLICY_VERSION
    original_required = module.SECONDARY_OCR_REQUIRED
    original_endpoint = module.SECONDARY_OCR_ENDPOINT
    original_ocr = module.ocr_engine.extract_ec8a
    original_vlm = module.vlm_engine.analyze_document
    original_docling = module.docling_engine.extract_tables
    original_secondary = module._secondary_ocr_consensus
    try:
        module.DOCUMENT_INTEGRITY_POLICY_VERSION = "ec8a-policy-v1"
        module.SECONDARY_OCR_REQUIRED = False
        module.SECONDARY_OCR_ENDPOINT = ""
        module.ocr_engine.extract_ec8a = lambda _: valid_ocr()
        module.vlm_engine.analyze_document = lambda *_: valid_vlm()
        module.docling_engine.extract_tables = lambda *_: valid_docling()
        module._secondary_ocr_consensus = lambda _: {
            "status": "not_configured",
            "engine": "got-ocr2.0",
        }

        result = module.analyze_evidence_bundle(
            document_image_bytes(), "ec8a.jpg", report_id=9
        )
        assert len(result["manifest_sha256"]) == 64
        assert len(result["image_evidence"]["content_sha256"]) == 64
        assert result["assessment_status"] == "analysis_complete"
        assert not result["requires_manual_review"]
        assert (
            result["decision"]
            == "evidence_collected_pending_authorized_result_workflow"
        )
        assert result["integrity_manifest"]["policy_version"] == "ec8a-policy-v1"

        module.vlm_engine.analyze_document = lambda *_: valid_vlm(tampering=True)
        flagged = module.analyze_evidence_bundle(
            document_image_bytes(), "ec8a.jpg", report_id=10
        )
        assert flagged["assessment_status"] == "manual_review_required"
        assert flagged["requires_manual_review"]
        assert any(
            finding["code"] == "vlm_tampering_indicator"
            and finding["severity"] == "critical"
            for finding in flagged["findings"]
        )

        module.SECONDARY_OCR_REQUIRED = True
        module.SECONDARY_OCR_ENDPOINT = ""
        module._secondary_ocr_consensus = original_secondary
        try:
            module._secondary_ocr_consensus(document_image_bytes())
        except HTTPException as exc:
            assert exc.status_code == 503
        else:
            raise AssertionError("required unavailable secondary OCR must fail closed")
    finally:
        module.DOCUMENT_INTEGRITY_POLICY_VERSION = original_policy
        module.SECONDARY_OCR_REQUIRED = original_required
        module.SECONDARY_OCR_ENDPOINT = original_endpoint
        module.ocr_engine.extract_ec8a = original_ocr
        module.vlm_engine.analyze_document = original_vlm
        module.docling_engine.extract_tables = original_docling
        module._secondary_ocr_consensus = original_secondary


if __name__ == "__main__":
    run()
    print("document evidence consensus regression: PASS")
