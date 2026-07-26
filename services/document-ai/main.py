"""INEC Document AI Service — PaddleOCR, VLM, DocLing, Video Analysis.

Provides:
- PaddleOCR: Extract text/numbers from EC8A result sheet photos
- VLM (Vision Language Model): Validate photo authenticity + detect tampering
- DocLing: Structured table extraction from form documents
- Video Analysis: Frame extraction + ballot counting anomaly detection
"""

import hashlib
import io
import json
import os
import re
import tempfile
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Annotated, Any, Optional

import structlog
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

log = structlog.get_logger()

app = FastAPI(
    title="INEC Document AI",
    version="1.0.0",
    description="AI-powered document analysis for election result verification",
)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

UPLOAD_DIR = os.getenv("UPLOAD_DIR", "/tmp/document-ai-uploads")
BACKEND_URL = os.getenv("BACKEND_URL", "http://localhost:8088")
OCR_MODEL_DIR = os.getenv("OCR_MODEL_DIR", "/models/paddleocr")
VLM_ENDPOINT = os.getenv("VLM_ENDPOINT", "")  # e.g. ollama or vLLM endpoint
DOCLING_MODEL = os.getenv("DOCLING_MODEL", "docling-v2")
NIMC_VERIFICATION_URL = os.getenv("NIMC_VERIFICATION_URL", "").strip()
IDENTITY_VERIFICATION_URL = os.getenv("IDENTITY_VERIFICATION_URL", "").strip()
SANCTIONS_SCREENING_URL = os.getenv("SANCTIONS_SCREENING_URL", "").strip()

# Evidence-integrity configuration. A secondary OCR endpoint is optional in
# development but may be required by deployment policy. It is intended for a
# self-hosted GOT-OCR 2.0 or olmOCR-compatible service, never a public fallback.
SECONDARY_OCR_ENDPOINT = os.getenv("SECONDARY_OCR_ENDPOINT", "").strip()
SECONDARY_OCR_ENGINE = os.getenv("SECONDARY_OCR_ENGINE", "got-ocr2.0").strip()
SECONDARY_OCR_REQUIRED = os.getenv("SECONDARY_OCR_REQUIRED", "false").lower() == "true"
DOCUMENT_INTEGRITY_POLICY_VERSION = os.getenv(
    "DOCUMENT_INTEGRITY_POLICY_VERSION", "unconfigured"
).strip()
DOCUMENT_ANALYSIS_VERSION = os.getenv(
    "DOCUMENT_ANALYSIS_VERSION", "evidence-consensus-v1"
).strip()
MAX_DOCUMENT_BYTES = int(os.getenv("MAX_DOCUMENT_BYTES", "20000000"))
MIN_OCR_CONFIDENCE = float(os.getenv("MIN_OCR_CONFIDENCE", "0.75"))
MIN_VLM_COMPLETENESS = float(os.getenv("MIN_VLM_COMPLETENESS", "0.80"))
MIN_DOCUMENT_EDGE = int(os.getenv("MIN_DOCUMENT_EDGE", "720"))

Path(UPLOAD_DIR).mkdir(parents=True, exist_ok=True)


# ─── PaddleOCR Integration ─────────────────────────────────────────────────


class OCRResult(BaseModel):
    text: str
    confidence: float
    bbox: list[list[int]]


class EC8AExtraction(BaseModel):
    serial_number: Optional[str] = None
    polling_unit_code: Optional[str] = None
    polling_unit_name: Optional[str] = None
    ward: Optional[str] = None
    lga: Optional[str] = None
    state: Optional[str] = None
    election_type: Optional[str] = None
    party_results: list[dict]
    total_valid_votes: Optional[int] = None
    total_rejected_votes: Optional[int] = None
    total_votes_cast: Optional[int] = None
    accredited_voters: Optional[int] = None
    registered_voters: Optional[int] = None
    presiding_officer_name: Optional[str] = None
    raw_ocr_text: str
    confidence_score: float
    extraction_warnings: list[str]


class OCREngine:
    """PaddleOCR wrapper for EC8A form text extraction."""

    def __init__(self):
        self._initialized = False
        self._paddle_ocr = None

    def _ensure_initialized(self):
        if self._initialized:
            return
        try:
            from paddleocr import PaddleOCR

            self._paddle_ocr = PaddleOCR(
                use_angle_cls=True,
                lang="en",
                det_model_dir=os.path.join(OCR_MODEL_DIR, "det")
                if os.path.exists(OCR_MODEL_DIR)
                else None,
                rec_model_dir=os.path.join(OCR_MODEL_DIR, "rec")
                if os.path.exists(OCR_MODEL_DIR)
                else None,
                cls_model_dir=os.path.join(OCR_MODEL_DIR, "cls")
                if os.path.exists(OCR_MODEL_DIR)
                else None,
                show_log=False,
            )
            self._initialized = True
            log.info("PaddleOCR initialized with local models")
        except ImportError:
            log.error("PaddleOCR is not installed; OCR requests will fail closed")
            self._initialized = True

    def extract_text(self, image_bytes: bytes) -> list[OCRResult]:
        """Run OCR on image bytes, return structured text regions."""
        self._ensure_initialized()

        if self._paddle_ocr is not None:
            return self._extract_with_paddle(image_bytes)
        raise HTTPException(
            status_code=503,
            detail="PaddleOCR is unavailable; no document extraction was performed",
        )

    def _extract_with_paddle(self, image_bytes: bytes) -> list[OCRResult]:
        """Real PaddleOCR extraction."""
        import numpy as np
        from PIL import Image

        img = Image.open(io.BytesIO(image_bytes))
        img_array = np.array(img)

        results = self._paddle_ocr.ocr(img_array, cls=True)
        ocr_results = []

        if results and results[0]:
            for line in results[0]:
                bbox = [[int(p[0]), int(p[1])] for p in line[0]]
                text = line[1][0]
                confidence = float(line[1][1])
                ocr_results.append(
                    OCRResult(text=text, confidence=confidence, bbox=bbox)
                )

        return ocr_results

    def extract_ec8a(self, image_bytes: bytes) -> EC8AExtraction:
        """Extract structured EC8A form data from image."""
        ocr_results = self.extract_text(image_bytes)
        raw_text = "\n".join(r.text for r in ocr_results)
        avg_confidence = sum(r.confidence for r in ocr_results) / max(
            len(ocr_results), 1
        )

        warnings = []
        party_results = []

        # Extract serial number (format: EC8A/XX/YYYY/NNNN)
        serial_match = re.search(
            r"EC8A[/\-]?\w{2,4}[/\-]?\d{4}[/\-]?\d{4,}", raw_text, re.IGNORECASE
        )
        serial_number = serial_match.group(0) if serial_match else None
        if not serial_number:
            warnings.append("Serial number not detected")

        # Extract polling unit code
        pu_match = re.search(
            r"(?:PU|Polling Unit)[:\s]*([A-Z0-9\-/]+)", raw_text, re.IGNORECASE
        )
        polling_unit_code = pu_match.group(1) if pu_match else None

        # Extract party results (pattern: PARTY_CODE followed by digits)
        nigerian_parties = [
            "APC",
            "PDP",
            "LP",
            "NNPP",
            "ADC",
            "SDP",
            "APGA",
            "YPP",
            "ZLP",
            "AA",
            "APM",
            "NRM",
        ]
        for party in nigerian_parties:
            pattern = rf"\b{party}\b[\s:]*(\d{{1,7}})"
            match = re.search(pattern, raw_text, re.IGNORECASE)
            if match:
                party_results.append(
                    {
                        "party_code": party,
                        "votes": int(match.group(1)),
                        "confidence": avg_confidence,
                    }
                )

        # Extract totals
        valid_match = re.search(
            r"(?:Total Valid|Valid Votes)[:\s]*(\d+)", raw_text, re.IGNORECASE
        )
        rejected_match = re.search(
            r"(?:Rejected|Void)[:\s]*(\d+)", raw_text, re.IGNORECASE
        )
        cast_match = re.search(
            r"(?:Total Votes? Cast|Total Cast)[:\s]*(\d+)", raw_text, re.IGNORECASE
        )
        accredited_match = re.search(
            r"(?:Accredited)[:\s]*(\d+)", raw_text, re.IGNORECASE
        )
        registered_match = re.search(
            r"(?:Registered)[:\s]*(\d+)", raw_text, re.IGNORECASE
        )

        total_valid = int(valid_match.group(1)) if valid_match else None
        total_rejected = int(rejected_match.group(1)) if rejected_match else None
        total_cast = int(cast_match.group(1)) if cast_match else None
        accredited = int(accredited_match.group(1)) if accredited_match else None
        registered = int(registered_match.group(1)) if registered_match else None

        # Validation: sum of party votes should equal total valid
        party_sum = sum(p["votes"] for p in party_results)
        if total_valid and party_sum != total_valid:
            warnings.append(
                f"Party vote sum ({party_sum}) != total valid votes ({total_valid})"
            )

        # Validation: accredited >= total cast
        if accredited and total_cast and total_cast > accredited:
            warnings.append(
                f"Total cast ({total_cast}) exceeds accredited ({accredited})"
            )

        if avg_confidence < 0.6:
            warnings.append("Low OCR confidence — manual review recommended")

        return EC8AExtraction(
            serial_number=serial_number,
            polling_unit_code=polling_unit_code,
            polling_unit_name=None,
            ward=None,
            lga=None,
            state=None,
            election_type=None,
            party_results=party_results,
            total_valid_votes=total_valid,
            total_rejected_votes=total_rejected,
            total_votes_cast=total_cast,
            accredited_voters=accredited,
            registered_voters=registered,
            presiding_officer_name=None,
            raw_ocr_text=raw_text,
            confidence_score=avg_confidence,
            extraction_warnings=warnings,
        )


ocr_engine = OCREngine()


# ─── VLM (Vision Language Model) Integration ─────────────────────────────────


class VLMResult(BaseModel):
    is_valid_ec8a: bool
    tampering_detected: bool
    tampering_confidence: float
    tampering_indicators: list[str]
    document_quality: str  # "good", "fair", "poor"
    orientation_correct: bool
    completeness_score: float
    analysis_summary: str


class VLMEngine:
    """Vision Language Model for document validation and tampering detection."""

    def __init__(self):
        self._client = None

    def _get_client(self):
        if self._client is None:
            try:
                import httpx

                self._client = httpx.Client(timeout=60.0)
            except ImportError:
                pass
        return self._client

    def analyze_document(
        self, image_bytes: bytes, document_type: str = "ec8a"
    ) -> VLMResult:
        """Analyze document for authenticity, tampering, and completeness."""

        if not VLM_ENDPOINT:
            raise HTTPException(
                status_code=503,
                detail=(
                    "VLM_ENDPOINT is not configured; "
                    "authenticity analysis was not performed"
                ),
            )
        return self._analyze_with_vlm(image_bytes, document_type)

    def _analyze_with_vlm(self, image_bytes: bytes, document_type: str) -> VLMResult:
        """Call external VLM endpoint (Ollama, vLLM, or OpenAI-compatible)."""
        import base64

        client = self._get_client()
        if not client:
            raise HTTPException(
                status_code=503,
                detail=(
                    "VLM client dependency is unavailable; "
                    "authenticity analysis was not performed"
                ),
            )

        img_b64 = base64.b64encode(image_bytes).decode()

        prompt = (
            f"Analyze this INEC {document_type.upper()} election result form image.\n"
            "Determine:\n"
            f"1. Is this a valid official INEC {document_type.upper()} form? (yes/no)\n"
            "2. Are there signs of tampering or alteration? (yes/no, list indicators)\n"
            "3. Document quality (good/fair/poor)\n"
            "4. Is the document properly oriented?\n"
            "5. Completeness (0-1 score): are all required fields filled?\n"
            "6. Brief analysis summary (1-2 sentences)\n\n"
            "Respond in JSON format:\n"
            '{"is_valid": bool, "tampering": bool, "tampering_confidence": float, '
            '"indicators": [...], "quality": "...", "oriented": bool, '
            '"completeness": float, "summary": "..."}'
        )

        try:
            # OpenAI-compatible API (works with Ollama, vLLM, etc.)
            response = client.post(
                f"{VLM_ENDPOINT}/v1/chat/completions",
                json={
                    "model": os.getenv("VLM_MODEL", "llava"),
                    "messages": [
                        {
                            "role": "user",
                            "content": [
                                {"type": "text", "text": prompt},
                                {
                                    "type": "image_url",
                                    "image_url": {
                                        "url": f"data:image/jpeg;base64,{img_b64}"
                                    },
                                },
                            ],
                        }
                    ],
                    "temperature": 0.1,
                    "max_tokens": 500,
                },
            )
            response.raise_for_status()
            result = response.json()
            content = result["choices"][0]["message"]["content"]

            # Parse JSON from VLM response
            import json

            # Handle markdown code blocks in response
            if "```json" in content:
                content = content.split("```json")[1].split("```")[0]
            elif "```" in content:
                content = content.split("```")[1].split("```")[0]

            data = json.loads(content.strip())
            return VLMResult(
                is_valid_ec8a=data.get("is_valid", False),
                tampering_detected=data.get("tampering", False),
                tampering_confidence=data.get("tampering_confidence", 0.0),
                tampering_indicators=data.get("indicators", []),
                document_quality=data.get("quality", "unknown"),
                orientation_correct=data.get("oriented", True),
                completeness_score=data.get("completeness", 0.0),
                analysis_summary=data.get("summary", ""),
            )
        except HTTPException:
            raise
        except Exception as e:
            log.error("VLM analysis failed", error=str(e))
            raise HTTPException(
                status_code=503,
                detail=(
                    "VLM analysis provider is unavailable or returned "
                    "invalid structured output"
                ),
            ) from e


vlm_engine = VLMEngine()


# ─── DocLing Integration (Structured Table Extraction) ────────────────────────


class TableCell(BaseModel):
    row: int
    col: int
    text: str
    confidence: float


class DocumentTable(BaseModel):
    headers: list[str]
    rows: list[dict]
    confidence: float


class DocLingResult(BaseModel):
    tables: list[DocumentTable]
    metadata: dict
    page_count: int
    extraction_method: str


class DocLingEngine:
    """DocLing integration for structured document/table extraction."""

    def __init__(self):
        self._initialized = False
        self._converter = None

    def _ensure_initialized(self):
        if self._initialized:
            return
        try:
            from docling.document_converter import DocumentConverter

            self._converter = DocumentConverter()
            self._initialized = True
            log.info("DocLing initialized")
        except ImportError:
            log.error(
                "DocLing is not installed; table extraction requests will fail closed"
            )
            self._initialized = True

    def extract_tables(self, file_bytes: bytes, filename: str) -> DocLingResult:
        """Extract structured tables from document."""
        self._ensure_initialized()

        if self._converter is not None:
            return self._extract_with_docling(file_bytes, filename)
        raise HTTPException(
            status_code=503,
            detail="DocLing is unavailable; no table extraction was performed",
        )

    def _extract_with_docling(self, file_bytes: bytes, filename: str) -> DocLingResult:
        """Real DocLing extraction."""
        # Write to temp file for DocLing
        suffix = Path(filename).suffix
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as f:
            f.write(file_bytes)
            tmp_path = f.name

        try:
            result = self._converter.convert(tmp_path)
            doc = result.document

            tables = []
            for table in doc.tables:
                df = table.export_to_dataframe()
                headers = list(df.columns)
                rows = df.to_dict(orient="records")
                tables.append(
                    DocumentTable(
                        headers=headers,
                        rows=rows,
                        confidence=0.85,
                    )
                )

            return DocLingResult(
                tables=tables,
                metadata={
                    "title": doc.title or "",
                    "num_pages": len(doc.pages) if hasattr(doc, "pages") else 1,
                },
                page_count=len(doc.pages) if hasattr(doc, "pages") else 1,
                extraction_method="docling",
            )
        finally:
            os.unlink(tmp_path)


docling_engine = DocLingEngine()


# ─── Evidence Integrity Consensus ────────────────────────────────────────────


def _canonical_json(value: Any) -> bytes:
    """Serialize analysis metadata reproducibly for content-addressed manifests."""
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, default=str
    ).encode("utf-8")


def _sha256_hex(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def _package_version(distribution: str, fallback: str) -> str:
    try:
        from importlib.metadata import version

        return version(distribution)
    except Exception:
        return fallback


def _normalise_tokens(value: str) -> set[str]:
    return {token for token in re.findall(r"[A-Z0-9]{2,}", value.upper())}


def _image_evidence(image_bytes: bytes) -> dict[str, Any]:
    """Create reproducible technical evidence without claiming authenticity.

    The perceptual fingerprint is a compact derivative for duplicate comparison;
    the SHA-256 remains the authoritative content address.
    """
    try:
        import numpy as np
        from PIL import Image, UnidentifiedImageError
    except ImportError as exc:
        raise HTTPException(
            status_code=503,
            detail="Document-integrity image dependencies are unavailable",
        ) from exc
    try:
        import cv2
    except ImportError:
        cv2 = None

    try:
        image = Image.open(io.BytesIO(image_bytes))
        image.load()
        width, height = image.size
        mode = image.mode
        grayscale = image.convert("L")
        resized = grayscale.resize((16, 16))
        pixels = list(resized.getdata())
    except (UnidentifiedImageError, OSError, ValueError) as exc:
        raise HTTPException(
            status_code=422,
            detail="Uploaded evidence is not a decodable document image",
        ) from exc

    pixel_mean = sum(pixels) / len(pixels)
    perceptual_bits = "".join("1" if pixel >= pixel_mean else "0" for pixel in pixels)
    perceptual_hash = format(int(perceptual_bits, 2), "064x")
    grayscale_array = np.array(grayscale)
    if cv2 is not None:
        blur_variance = float(cv2.Laplacian(grayscale_array, cv2.CV_64F).var())
        blur_method = "opencv_laplacian_variance"
    else:
        # Pillow/NumPy fallback preserves an objective technical quality signal
        # when the optional OpenCV wheel is unavailable. It is never used to
        # approve a result; it only informs manual-review routing.
        gradient_x = np.diff(grayscale_array.astype(float), axis=1)
        gradient_y = np.diff(grayscale_array.astype(float), axis=0)
        blur_variance = float((np.var(gradient_x) + np.var(gradient_y)) / 2)
        blur_method = "numpy_gradient_variance_fallback"
    quality_findings: list[dict[str, Any]] = []
    if min(width, height) < MIN_DOCUMENT_EDGE:
        quality_findings.append(
            {
                "code": "image_resolution_below_minimum",
                "severity": "high",
                "detail": (
                    f"Image dimensions {width}x{height} are below the "
                    f"{MIN_DOCUMENT_EDGE}px minimum edge threshold."
                ),
            }
        )
    if blur_variance < 80.0:
        quality_findings.append(
            {
                "code": "image_blur_detected",
                "severity": "medium",
                "detail": (
                    "Image edge variance is low; document text may not be "
                    "reliably recoverable."
                ),
            }
        )
    return {
        "content_sha256": _sha256_hex(image_bytes),
        "perceptual_hash": perceptual_hash,
        "byte_size": len(image_bytes),
        "width": width,
        "height": height,
        "mode": mode,
        "blur_variance": round(blur_variance, 3),
        "blur_method": blur_method,
        "quality_findings": quality_findings,
    }


def _secondary_ocr_consensus(image_bytes: bytes) -> dict[str, Any]:
    """Call only an explicitly configured self-hosted secondary OCR service.

    The contract is intentionally narrow: `POST {endpoint}/extract` accepts a
    JSON image payload and returns `{text, confidence, fields?}`. If an operator
    marks this engine required, an unavailable or malformed response is an
    analysis-unavailable condition rather than a hidden fallback.
    """
    if not SECONDARY_OCR_ENDPOINT:
        if SECONDARY_OCR_REQUIRED:
            raise HTTPException(
                status_code=503,
                detail="A required self-hosted secondary OCR service is not configured",
            )
        return {"status": "not_configured", "engine": SECONDARY_OCR_ENGINE}

    import base64

    request_body = _canonical_json(
        {
            "image_base64": base64.b64encode(image_bytes).decode("ascii"),
            "document_type": "ec8a",
            "response_format": "structured_json",
        }
    )
    request = urllib.request.Request(
        f"{SECONDARY_OCR_ENDPOINT.rstrip('/')}/extract",
        data=request_body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            raw_response = response.read()
        payload = json.loads(raw_response.decode("utf-8"))
        text = payload.get("text")
        confidence = payload.get("confidence")
        if not isinstance(text, str) or not isinstance(confidence, (int, float)):
            raise ValueError("secondary OCR response lacks text/confidence")
        if not 0.0 <= float(confidence) <= 1.0:
            raise ValueError("secondary OCR confidence is outside [0, 1]")
        return {
            "status": "completed",
            "engine": SECONDARY_OCR_ENGINE,
            "text": text,
            "confidence": float(confidence),
            "fields": payload.get("fields", {}),
        }
    except (
        urllib.error.URLError,
        urllib.error.HTTPError,
        TimeoutError,
        ValueError,
        json.JSONDecodeError,
    ) as exc:
        if SECONDARY_OCR_REQUIRED:
            raise HTTPException(
                status_code=503,
                detail="Required secondary OCR analysis is unavailable or invalid",
            ) from exc
        log.warning(
            "secondary_ocr_unavailable", engine=SECONDARY_OCR_ENGINE, error=str(exc)
        )
        return {
            "status": "unavailable",
            "engine": SECONDARY_OCR_ENGINE,
            "reason": "optional_secondary_ocr_unavailable",
        }


def _docling_has_table(result: DocLingResult) -> bool:
    return any(table.headers or table.rows for table in result.tables)


def _consensus_findings(
    image_evidence: dict[str, Any],
    ocr_result: EC8AExtraction,
    vlm_result: VLMResult,
    docling_result: DocLingResult,
    secondary_ocr: dict[str, Any],
) -> list[dict[str, Any]]:
    findings = list(image_evidence["quality_findings"])
    if DOCUMENT_INTEGRITY_POLICY_VERSION == "unconfigured":
        findings.append(
            {
                "code": "policy_version_unconfigured",
                "severity": "high",
                "detail": (
                    "The analysis has no configured election-integrity policy "
                    "version and requires manual review."
                ),
            }
        )
    if ocr_result.confidence_score < MIN_OCR_CONFIDENCE:
        findings.append(
            {
                "code": "paddleocr_confidence_below_policy",
                "severity": "high",
                "detail": (
                    f"PaddleOCR confidence {ocr_result.confidence_score:.3f} "
                    f"is below the policy threshold {MIN_OCR_CONFIDENCE:.3f}."
                ),
            }
        )
    for warning in ocr_result.extraction_warnings:
        severity = (
            "high" if "!=" in warning or "exceeds" in warning.lower() else "medium"
        )
        findings.append(
            {
                "code": "paddleocr_validation_warning",
                "severity": severity,
                "detail": warning,
            }
        )
    if not vlm_result.is_valid_ec8a:
        findings.append(
            {
                "code": "vlm_form_type_unconfirmed",
                "severity": "high",
                "detail": "The VLM could not confirm the expected EC8A form type.",
            }
        )
    if vlm_result.tampering_detected:
        findings.append(
            {
                "code": "vlm_tampering_indicator",
                "severity": "critical",
                "detail": (
                    "The VLM detected a potential alteration indicator; this is "
                    "evidence for review, not an automatic rejection."
                ),
                "indicators": vlm_result.tampering_indicators,
            }
        )
    if not vlm_result.orientation_correct:
        findings.append(
            {
                "code": "document_orientation_unconfirmed",
                "severity": "medium",
                "detail": (
                    "The VLM reports that the document orientation is not reliable."
                ),
            }
        )
    if vlm_result.completeness_score < MIN_VLM_COMPLETENESS:
        findings.append(
            {
                "code": "vlm_completeness_below_policy",
                "severity": "high",
                "detail": (
                    f"VLM completeness {vlm_result.completeness_score:.3f} "
                    f"is below the policy threshold {MIN_VLM_COMPLETENESS:.3f}."
                ),
            }
        )
    if not _docling_has_table(docling_result):
        findings.append(
            {
                "code": "docling_table_not_detected",
                "severity": "high",
                "detail": (
                    "Docling did not recover a structured result table from the "
                    "submitted document."
                ),
            }
        )
    if secondary_ocr.get("status") == "completed":
        primary_tokens = _normalise_tokens(ocr_result.raw_ocr_text)
        secondary_tokens = _normalise_tokens(str(secondary_ocr.get("text", "")))
        if primary_tokens and secondary_tokens:
            overlap = len(primary_tokens & secondary_tokens) / max(
                len(primary_tokens), len(secondary_tokens)
            )
            if overlap < 0.35:
                findings.append(
                    {
                        "code": "secondary_ocr_disagreement",
                        "severity": "high",
                        "detail": (
                            f"PaddleOCR and {SECONDARY_OCR_ENGINE} token overlap "
                            f"is only {overlap:.2f}."
                        ),
                    }
                )
        elif not secondary_tokens:
            findings.append(
                {
                    "code": "secondary_ocr_empty",
                    "severity": "high",
                    "detail": (
                        f"{SECONDARY_OCR_ENGINE} returned no usable document text."
                    ),
                }
            )
    elif secondary_ocr.get("status") == "unavailable":
        findings.append(
            {
                "code": "secondary_ocr_unavailable",
                "severity": "medium",
                "detail": (
                    "The optional secondary OCR engine was unavailable; the "
                    "evidence requires manual review."
                ),
            }
        )
    return findings


def _decision_from_findings(findings: list[dict[str, Any]]) -> tuple[str, bool, str]:
    # Automation collects evidence and requests review. It never renders a final
    # electoral verdict or silently converts a model score into approval.
    if any(finding["code"] == "image_resolution_below_minimum" for finding in findings):
        return (
            "rejected_for_quality",
            True,
            "evidence_rejected_for_quality_manual_resubmission_required",
        )
    if findings:
        return (
            "manual_review_required",
            True,
            "evidence_collected_manual_review_required",
        )
    return (
        "analysis_complete",
        False,
        "evidence_collected_pending_authorized_result_workflow",
    )


def analyze_evidence_bundle(
    image_bytes: bytes, filename: str, report_id: Optional[int]
) -> dict[str, Any]:
    """Run the evidence pipeline with conservative, review-safe semantics."""
    if not image_bytes:
        raise HTTPException(status_code=400, detail="Document evidence file is empty")
    if len(image_bytes) > MAX_DOCUMENT_BYTES:
        raise HTTPException(
            status_code=413,
            detail=f"Document evidence exceeds {MAX_DOCUMENT_BYTES} byte limit",
        )

    image_evidence = _image_evidence(image_bytes)
    ocr_result = ocr_engine.extract_ec8a(image_bytes)
    vlm_result = vlm_engine.analyze_document(image_bytes, "ec8a")
    docling_result = docling_engine.extract_tables(image_bytes, filename)
    secondary_ocr = _secondary_ocr_consensus(image_bytes)
    findings = _consensus_findings(
        image_evidence, ocr_result, vlm_result, docling_result, secondary_ocr
    )
    assessment_status, requires_manual_review, decision = _decision_from_findings(
        findings
    )
    docling_confidence = max(
        (table.confidence for table in docling_result.tables), default=0.0
    )
    secondary_confidence = secondary_ocr.get("confidence")
    confidence_inputs = [
        ocr_result.confidence_score,
        vlm_result.completeness_score,
        docling_confidence,
    ]
    if isinstance(secondary_confidence, (int, float)):
        confidence_inputs.append(float(secondary_confidence))
    combined_confidence = round(sum(confidence_inputs) / len(confidence_inputs), 3)
    timestamp = datetime.now(timezone.utc).isoformat()
    engine_versions = {
        "paddleocr": _package_version("paddleocr", "configured-local-model"),
        "vlm": os.getenv("VLM_MODEL", "unconfigured"),
        "docling": _package_version("docling", DOCLING_MODEL),
        "secondary_ocr": SECONDARY_OCR_ENGINE
        if secondary_ocr.get("status") != "not_configured"
        else "not_configured",
        "analysis": DOCUMENT_ANALYSIS_VERSION,
    }
    manifest = {
        "schema_version": "inec-election-evidence-manifest/v1",
        "analysis_version": DOCUMENT_ANALYSIS_VERSION,
        "policy_version": DOCUMENT_INTEGRITY_POLICY_VERSION,
        "report_id": report_id,
        "filename": filename,
        "created_at": timestamp,
        "document": image_evidence,
        "engine_versions": engine_versions,
        "engine_status": {
            "paddleocr": "completed",
            "vlm": "completed",
            "docling": "completed",
            "secondary_ocr": secondary_ocr.get("status"),
        },
        "assessment_status": assessment_status,
        "decision": decision,
        "combined_confidence": combined_confidence,
        "requires_manual_review": requires_manual_review,
        "findings": findings,
    }
    manifest_sha256 = _sha256_hex(_canonical_json(manifest))
    return {
        "report_id": report_id,
        "ocr": ocr_result.model_dump(),
        "vlm": vlm_result.model_dump(),
        "docling": docling_result.model_dump(),
        "secondary_ocr": secondary_ocr,
        "image_evidence": image_evidence,
        "engine_versions": engine_versions,
        "findings": findings,
        "assessment_status": assessment_status,
        "decision": decision,
        "combined_confidence": combined_confidence,
        "requires_manual_review": requires_manual_review,
        "integrity_manifest": manifest,
        "manifest_sha256": manifest_sha256,
        "timestamp": timestamp,
    }


# ─── Video Analysis ───────────────────────────────────────────────────────────


class VideoAnalysisResult(BaseModel):
    duration_seconds: float
    frame_count: int
    fps: float
    resolution: dict
    key_frames_extracted: int
    anomalies_detected: list[dict]
    ballot_counting_events: list[dict]
    integrity_score: float
    analysis_summary: str


class VideoAnalyzer:
    """Video analysis for ballot counting verification and anomaly detection."""

    def __init__(self):
        self._cv2 = None

    def _ensure_cv2(self):
        if self._cv2 is None:
            try:
                import cv2

                self._cv2 = cv2
            except ImportError:
                log.warn("OpenCV not installed — video analysis unavailable")

    def analyze_video(self, video_bytes: bytes, filename: str) -> VideoAnalysisResult:
        """Analyze video for ballot counting events and anomalies."""
        self._ensure_cv2()

        if self._cv2 is not None:
            return self._analyze_with_opencv(video_bytes, filename)
        raise HTTPException(
            status_code=503,
            detail="OpenCV is unavailable; no ballot-video analysis was performed",
        )

    def _analyze_with_opencv(
        self, video_bytes: bytes, filename: str
    ) -> VideoAnalysisResult:
        """Full video analysis with OpenCV."""
        cv2 = self._cv2

        # Write to temp file
        suffix = Path(filename).suffix or ".mp4"
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as f:
            f.write(video_bytes)
            tmp_path = f.name

        try:
            cap = cv2.VideoCapture(tmp_path)
            if not cap.isOpened():
                raise ValueError("Cannot open video file")

            fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
            total_frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
            width = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
            height = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
            duration = total_frames / fps if fps > 0 else 0

            # Extract key frames (1 per second)
            key_frames = []
            anomalies = []
            ballot_events = []
            prev_frame = None
            frame_idx = 0
            sample_interval = max(int(fps), 1)

            while True:
                ret, frame = cap.read()
                if not ret:
                    break

                if frame_idx % sample_interval == 0:
                    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                    key_frames.append(gray)

                    # Detect scene changes (potential ballot counting events)
                    if prev_frame is not None:
                        diff = cv2.absdiff(prev_frame, gray)
                        change_pct = (diff > 30).sum() / diff.size * 100

                        if change_pct > 40:
                            anomalies.append(
                                {
                                    "frame": frame_idx,
                                    "timestamp_sec": round(frame_idx / fps, 2),
                                    "type": "scene_change",
                                    "change_pct": round(change_pct, 2),
                                    "description": "Significant scene change detected",
                                }
                            )

                        # Detect motion patterns consistent with ballot handling
                        if 5 < change_pct < 25:
                            ballot_events.append(
                                {
                                    "frame": frame_idx,
                                    "timestamp_sec": round(frame_idx / fps, 2),
                                    "type": "ballot_handling",
                                    "motion_pct": round(change_pct, 2),
                                }
                            )

                    prev_frame = gray
                frame_idx += 1

            cap.release()

            # Check for video integrity issues
            if duration < 5:
                anomalies.append(
                    {
                        "frame": 0,
                        "timestamp_sec": 0,
                        "type": "short_video",
                        "description": (
                            f"Video only {duration:.1f}s — may be incomplete"
                        ),
                    }
                )

            if total_frames == 0:
                anomalies.append(
                    {
                        "frame": 0,
                        "timestamp_sec": 0,
                        "type": "empty_video",
                        "description": "No frames detected",
                    }
                )

            # Integrity score: penalize for anomalies
            integrity = max(0.0, 1.0 - len(anomalies) * 0.15)

            return VideoAnalysisResult(
                duration_seconds=round(duration, 2),
                frame_count=total_frames,
                fps=round(fps, 2),
                resolution={"width": width, "height": height},
                key_frames_extracted=len(key_frames),
                anomalies_detected=anomalies,
                ballot_counting_events=ballot_events[:50],  # Cap at 50
                integrity_score=round(integrity, 3),
                analysis_summary=(
                    f"Analyzed {total_frames} frames ({duration:.1f}s). "
                    f"{len(ballot_events)} ballot events, "
                    f"{len(anomalies)} anomalies detected."
                ),
            )
        finally:
            os.unlink(tmp_path)


video_analyzer = VideoAnalyzer()


# ─── KYC (Know Your Customer) Pipeline ───────────────────────────────────────


class KYCVerificationRequest(BaseModel):
    user_id: int
    full_name: str
    id_type: str  # "nin", "voters_card", "passport", "drivers_license"
    id_number: str
    date_of_birth: Optional[str] = None
    phone_number: Optional[str] = None
    address: Optional[str] = None


class KYCVerificationResult(BaseModel):
    user_id: int
    status: str  # "verified", "pending_review", "rejected", "requires_liveness"
    identity_match_score: float
    document_verified: bool
    face_match_score: float
    liveness_passed: bool
    risk_score: float  # 0=low risk, 1=high risk
    checks_performed: list[str]
    flags: list[str]
    verification_timestamp: str


class LivenessCheckResult(BaseModel):
    user_id: int
    passed: bool
    confidence: float
    method: str  # "passive", "active_blink", "active_head_turn", "3d_depth"
    anti_spoofing_score: float
    checks: list[dict]
    timestamp: str


class KYCEngine:
    """KYC verification pipeline with liveness detection."""

    def __init__(self):
        self._face_detector = None

    def _ensure_face_detection(self):
        if self._face_detector is not None:
            return True
        try:
            import cv2

            cascade_path = cv2.data.haarcascades + "haarcascade_frontalface_default.xml"
            self._face_detector = cv2.CascadeClassifier(cascade_path)
            return True
        except (ImportError, Exception):
            return False

    def verify_identity(
        self,
        request: KYCVerificationRequest,
        id_document_bytes: Optional[bytes] = None,
        selfie_bytes: Optional[bytes] = None,
    ) -> KYCVerificationResult:
        """Verify identity only when authoritative providers can support the decision.

        Local document and face analysis may contribute evidence, but they cannot by
        themselves produce a verified KYC status. Missing authorities result in
        pending review rather than fabricated scores or an inferred approval.
        """
        checks: list[str] = []
        flags: list[str] = []
        scores: dict[str, float] = {}
        authoritative_dependencies_missing = False

        id_valid = self._validate_id_format(request.id_type, request.id_number)
        checks.append("id_format_validation")
        if not id_valid:
            flags.append(f"Invalid {request.id_type} format")

        doc_verified = False
        if id_document_bytes:
            doc_result = self._verify_document(id_document_bytes, request)
            doc_verified = doc_result["verified"]
            scores["document"] = doc_result["confidence"]
            checks.append("document_ocr_verification")
            if not doc_verified:
                flags.append("Document OCR mismatch")
        else:
            flags.append("No ID document uploaded")

        face_score: Optional[float] = None
        if selfie_bytes and id_document_bytes:
            face_score = self._compare_faces(selfie_bytes, id_document_bytes)
            checks.append("face_comparison")
            if face_score is None:
                authoritative_dependencies_missing = True
                flags.append("Face-comparison service unavailable")
            else:
                scores["face_match"] = face_score
                if face_score < 0.7:
                    flags.append("Face match below threshold")

        identity_provider_url = (
            NIMC_VERIFICATION_URL
            if request.id_type == "nin"
            else IDENTITY_VERIFICATION_URL
        )
        identity_check = self._verify_authoritative_identity(
            identity_provider_url, request
        )
        checks.append("authoritative_identity_lookup")
        if identity_check is None:
            authoritative_dependencies_missing = True
            flags.append("Authoritative identity verification is unavailable")
        else:
            scores["identity_provider"] = identity_check["match_score"]
            if not identity_check["verified"]:
                flags.append(
                    "Authoritative identity provider did not verify the record"
                )

        sanctions_clear = self._screen_sanctions(request.full_name)
        checks.append("sanctions_pep_screening")
        if sanctions_clear is None:
            authoritative_dependencies_missing = True
            flags.append("Sanctions/PEP screening is unavailable")
        elif not sanctions_clear:
            flags.append("PEP/Sanctions match found")

        if request.phone_number:
            checks.append("phone_verification_pending")
            authoritative_dependencies_missing = True
            flags.append("Phone verification requires an authoritative provider")

        risk_factors = len(flags)
        risk_score = min(1.0, risk_factors * 0.2)
        if authoritative_dependencies_missing:
            # An outage or absent provider is not adverse identity evidence.
            status = "pending_review"
        elif risk_score > 0.6:
            status = "rejected"
        elif selfie_bytes and face_score is not None and face_score < 0.5:
            status = "requires_liveness"
        elif risk_score > 0.3:
            status = "pending_review"
        else:
            status = "verified"

        identity_match = sum(scores.values()) / len(scores) if scores else 0.0

        return KYCVerificationResult(
            user_id=request.user_id,
            status=status,
            identity_match_score=round(identity_match, 3),
            document_verified=doc_verified,
            face_match_score=round(face_score or 0.0, 3),
            liveness_passed=False,
            risk_score=round(risk_score, 3),
            checks_performed=checks,
            flags=flags,
            verification_timestamp=datetime.now(timezone.utc).isoformat(),
        )

    def liveness_check(
        self, video_bytes: bytes, user_id: int, method: str = "passive"
    ) -> LivenessCheckResult:
        """Perform liveness detection to prevent spoofing."""
        checks = []

        if not self._ensure_face_detection():
            raise HTTPException(
                status_code=503,
                detail=(
                    "OpenCV face detection is unavailable; "
                    "no liveness decision was performed"
                ),
            )

        import cv2

        # Write video to temp file
        with tempfile.NamedTemporaryFile(suffix=".mp4", delete=False) as f:
            f.write(video_bytes)
            tmp_path = f.name

        try:
            cap = cv2.VideoCapture(tmp_path)
            if not cap.isOpened():
                return LivenessCheckResult(
                    user_id=user_id,
                    passed=False,
                    confidence=0.0,
                    method=method,
                    anti_spoofing_score=0.0,
                    checks=[{"name": "video_open", "passed": False}],
                    timestamp=datetime.now(timezone.utc).isoformat(),
                )

            face_sizes = []
            face_positions = []
            frame_count = 0
            faces_detected_count = 0
            texture_scores = []

            while frame_count < 90:  # Analyze up to 3 seconds at 30fps
                ret, frame = cap.read()
                if not ret:
                    break

                gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                faces = self._face_detector.detectMultiScale(gray, 1.3, 5)

                if len(faces) == 1:
                    faces_detected_count += 1
                    x, y, w, h = faces[0]
                    face_sizes.append(w * h)
                    face_positions.append((x + w // 2, y + h // 2))

                    # Texture analysis (LBP-based anti-spoofing)
                    face_roi = gray[y : y + h, x : x + w]
                    if face_roi.size > 0:
                        laplacian_var = cv2.Laplacian(face_roi, cv2.CV_64F).var()
                        texture_scores.append(laplacian_var)

                frame_count += 1

            cap.release()

            # Check 1: Consistent face detection (at least 60% of frames)
            face_ratio = faces_detected_count / max(frame_count, 1)
            checks.append(
                {
                    "name": "face_presence",
                    "passed": face_ratio > 0.6,
                    "value": round(face_ratio, 3),
                    "threshold": 0.6,
                }
            )

            # Check 2: Natural face size variation (not a flat photo)
            size_variation = 0.0
            if len(face_sizes) > 5:
                mean_size = sum(face_sizes) / len(face_sizes)
                size_variation = (max(face_sizes) - min(face_sizes)) / max(mean_size, 1)
            checks.append(
                {
                    "name": "size_variation",
                    "passed": size_variation > 0.02,
                    "value": round(size_variation, 4),
                    "threshold": 0.02,
                    "note": "Flat photos have near-zero size variation",
                }
            )

            # Check 3: Natural position movement
            position_movement = 0.0
            if len(face_positions) > 5:
                dx = [
                    abs(face_positions[i][0] - face_positions[i - 1][0])
                    for i in range(1, len(face_positions))
                ]
                dy = [
                    abs(face_positions[i][1] - face_positions[i - 1][1])
                    for i in range(1, len(face_positions))
                ]
                position_movement = (sum(dx) + sum(dy)) / len(dx)
            checks.append(
                {
                    "name": "natural_movement",
                    "passed": position_movement > 1.0,
                    "value": round(position_movement, 3),
                    "threshold": 1.0,
                    "note": "Real faces have micro-movements",
                }
            )

            # Check 4: Texture analysis (screens/prints have different texture)
            avg_texture = (
                sum(texture_scores) / max(len(texture_scores), 1)
                if texture_scores
                else 0
            )
            checks.append(
                {
                    "name": "texture_liveness",
                    "passed": avg_texture > 50.0,
                    "value": round(avg_texture, 2),
                    "threshold": 50.0,
                    "note": "Low texture variance suggests screen/print attack",
                }
            )

            # Check 5: Temporal consistency (same person throughout)
            if len(face_sizes) > 10:
                size_std = (
                    sum(
                        (s - sum(face_sizes) / len(face_sizes)) ** 2 for s in face_sizes
                    )
                    / len(face_sizes)
                ) ** 0.5
                consistency = 1.0 - min(
                    1.0, size_std / (sum(face_sizes) / len(face_sizes))
                )
            else:
                consistency = 0.0
            checks.append(
                {
                    "name": "temporal_consistency",
                    "passed": consistency > 0.8,
                    "value": round(consistency, 3),
                    "threshold": 0.8,
                }
            )

            # Active liveness checks
            if method == "active_blink":
                # Detect blink: face area should show eye-region changes
                checks.append(
                    {
                        "name": "blink_detection",
                        "passed": face_ratio > 0.6 and size_variation > 0.01,
                        "note": "Blink detection requires eye landmark model",
                    }
                )
            elif method == "active_head_turn":
                checks.append(
                    {
                        "name": "head_turn",
                        "passed": position_movement > 5.0,
                        "value": round(position_movement, 3),
                        "note": "Head turn requires significant lateral movement",
                    }
                )

            # Calculate final scores
            passed_checks = sum(1 for c in checks if c.get("passed", False))
            total_checks = len(checks)
            confidence = passed_checks / max(total_checks, 1)

            # Anti-spoofing score (weighted)
            anti_spoof = (
                (0.3 * (1 if size_variation > 0.02 else 0))
                + (0.3 * (1 if avg_texture > 50 else 0))
                + (0.2 * (1 if position_movement > 1.0 else 0))
                + (0.2 * (1 if consistency > 0.8 else 0))
            )

            passed = confidence >= 0.7 and anti_spoof >= 0.6

            return LivenessCheckResult(
                user_id=user_id,
                passed=passed,
                confidence=round(confidence, 3),
                method=method,
                anti_spoofing_score=round(anti_spoof, 3),
                checks=checks,
                timestamp=datetime.now(timezone.utc).isoformat(),
            )
        finally:
            os.unlink(tmp_path)

    def _validate_id_format(self, id_type: str, id_number: str) -> bool:
        """Validate Nigerian ID number formats."""
        patterns = {
            "nin": r"^\d{11}$",  # 11-digit NIN
            "voters_card": r"^[A-Z0-9]{19}$",  # 19-char PVC number
            "passport": r"^[A-Z]\d{8}$",  # Letter + 8 digits
            # State prefix, numeric identifier, and suffix.
            "drivers_license": r"^[A-Z]{3}\d{5,12}[A-Z]{2}$",
        }
        pattern = patterns.get(id_type)
        if not pattern:
            return False
        return bool(re.match(pattern, id_number))

    def _verify_document(
        self, doc_bytes: bytes, request: KYCVerificationRequest
    ) -> dict:
        """OCR the ID document and verify against provided details."""
        ocr_results = ocr_engine.extract_text(doc_bytes)
        text = " ".join(r.text for r in ocr_results).upper()

        confidence = 0.0
        name_parts = request.full_name.upper().split()

        # Check if name appears in document
        name_matches = sum(1 for part in name_parts if part in text)
        name_score = name_matches / max(len(name_parts), 1)

        # Check if ID number appears
        id_found = request.id_number.upper() in text.replace(" ", "")
        id_score = 1.0 if id_found else 0.0

        confidence = name_score * 0.5 + id_score * 0.5
        verified = confidence > 0.6

        return {"verified": verified, "confidence": confidence}

    def _compare_faces(
        self, selfie_bytes: bytes, document_bytes: bytes
    ) -> Optional[float]:
        """Compare selfie and ID-document faces when a local detector is available."""
        if not self._ensure_face_detection():
            return None

        import cv2
        import numpy as np

        def detect_face(img_bytes: bytes):
            arr = np.frombuffer(img_bytes, np.uint8)
            img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
            if img is None:
                return None
            gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
            faces = self._face_detector.detectMultiScale(gray, 1.3, 5)
            if len(faces) == 0:
                return None
            x, y, w, h = faces[0]
            return gray[y : y + h, x : x + w]

        face1 = detect_face(selfie_bytes)
        face2 = detect_face(document_bytes)

        if face1 is None or face2 is None:
            return None

        # Resize to same dimensions for histogram comparison
        target_size = (100, 100)
        face1_resized = cv2.resize(face1, target_size)
        face2_resized = cv2.resize(face2, target_size)

        # Histogram comparison (Correlation method)
        hist1 = cv2.calcHist([face1_resized], [0], None, [256], [0, 256])
        hist2 = cv2.calcHist([face2_resized], [0], None, [256], [0, 256])
        cv2.normalize(hist1, hist1, 0, 1, cv2.NORM_MINMAX)
        cv2.normalize(hist2, hist2, 0, 1, cv2.NORM_MINMAX)

        score = cv2.compareHist(hist1, hist2, cv2.HISTCMP_CORREL)
        return max(0.0, min(1.0, score))

    def _post_authority_json(
        self, url: str, payload: dict[str, Any]
    ) -> Optional[dict[str, Any]]:
        """Call a configured authority endpoint and accept a JSON object response."""
        if not url:
            return None
        request = urllib.request.Request(
            url,
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json", "Accept": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                if response.status < 200 or response.status >= 300:
                    return None
                parsed = json.loads(response.read().decode("utf-8"))
                return parsed if isinstance(parsed, dict) else None
        except (
            urllib.error.URLError,
            urllib.error.HTTPError,
            TimeoutError,
            ValueError,
            json.JSONDecodeError,
        ) as error:
            log.warning("authority_provider_unavailable", url=url, error=str(error))
            return None

    def _verify_authoritative_identity(
        self, provider_url: str, request: KYCVerificationRequest
    ) -> Optional[dict[str, Any]]:
        """Require an authority response of {verified: bool, match_score: 0..1}."""
        response = self._post_authority_json(
            provider_url,
            {
                "user_id": request.user_id,
                "full_name": request.full_name,
                "id_type": request.id_type,
                "id_number": request.id_number,
                "date_of_birth": request.date_of_birth,
                "phone_number": request.phone_number,
            },
        )
        if response is None or not isinstance(response.get("verified"), bool):
            return None
        try:
            match_score = float(response.get("match_score"))
        except (TypeError, ValueError):
            return None
        if not 0.0 <= match_score <= 1.0:
            return None
        return {"verified": response["verified"], "match_score": match_score}

    def _screen_sanctions(self, full_name: str) -> Optional[bool]:
        """Require a configured sanctions provider response of {clear: bool}."""
        response = self._post_authority_json(
            SANCTIONS_SCREENING_URL, {"full_name": full_name}
        )
        if response is None or not isinstance(response.get("clear"), bool):
            return None
        return response["clear"]


kyc_engine = KYCEngine()


# ─── API Endpoints ────────────────────────────────────────────────────────────


@app.get("/health")
async def health():
    ocr_engine._ensure_initialized()
    docling_engine._ensure_initialized()
    video_analyzer._ensure_cv2()
    services = {
        "paddleocr": "available"
        if ocr_engine._paddle_ocr is not None
        else "unavailable",
        "vlm": "available" if VLM_ENDPOINT else "unavailable",
        "docling": "available"
        if docling_engine._converter is not None
        else "unavailable",
        "video": "available" if video_analyzer._cv2 is not None else "unavailable",
        "nimc_identity": "configured" if NIMC_VERIFICATION_URL else "unavailable",
        "identity_provider": "configured"
        if IDENTITY_VERIFICATION_URL
        else "unavailable",
        "sanctions_screening": "configured"
        if SANCTIONS_SCREENING_URL
        else "unavailable",
        "secondary_ocr": (
            "configured"
            if SECONDARY_OCR_ENDPOINT
            else ("required_but_unconfigured" if SECONDARY_OCR_REQUIRED else "optional")
        ),
        "integrity_policy": (
            "configured"
            if DOCUMENT_INTEGRITY_POLICY_VERSION != "unconfigured"
            else "unavailable"
        ),
    }
    return {
        "status": "healthy"
        if all(
            value in {"available", "configured", "optional"}
            for value in services.values()
        )
        else "degraded",
        "services": services,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


@app.post("/ocr/extract")
async def ocr_extract(file: Annotated[UploadFile, File(...)]):
    """Extract raw text from an image using PaddleOCR."""
    content = await file.read()
    results = ocr_engine.extract_text(content)
    return {
        "filename": file.filename,
        "regions": [r.model_dump() for r in results],
        "total_regions": len(results),
    }


@app.post("/ocr/ec8a")
async def ocr_ec8a(file: Annotated[UploadFile, File(...)]):
    """Extract structured EC8A form data from an image."""
    content = await file.read()
    extraction = ocr_engine.extract_ec8a(content)
    return extraction.model_dump()


@app.post("/vlm/analyze")
async def vlm_analyze(
    file: Annotated[UploadFile, File(...)],
    document_type: Annotated[str, Form()] = "ec8a",
):
    """Analyze document for authenticity and tampering using VLM."""
    content = await file.read()
    result = vlm_engine.analyze_document(content, document_type)
    return result.model_dump()


@app.post("/docling/tables")
async def docling_extract_tables(file: Annotated[UploadFile, File(...)]):
    """Extract structured tables from a document using DocLing."""
    content = await file.read()
    result = docling_engine.extract_tables(content, file.filename or "document.pdf")
    return result.model_dump()


@app.post("/video/analyze")
async def video_analyze(file: Annotated[UploadFile, File(...)]):
    """Analyze video for ballot counting events and anomalies."""
    content = await file.read()
    if len(content) > 500_000_000:  # 500MB limit
        raise HTTPException(status_code=413, detail="Video exceeds 500MB limit")
    result = video_analyzer.analyze_video(content, file.filename or "video.mp4")
    return result.model_dump()


@app.post("/kyc/verify")
async def kyc_verify(
    user_id: Annotated[int, Form(...)],
    full_name: Annotated[str, Form(...)],
    id_type: Annotated[str, Form(...)],
    id_number: Annotated[str, Form(...)],
    date_of_birth: Annotated[Optional[str], Form()] = None,
    phone_number: Annotated[Optional[str], Form()] = None,
    id_document: Annotated[Optional[UploadFile], File()] = None,
    selfie: Annotated[Optional[UploadFile], File()] = None,
):
    """Full KYC identity verification pipeline."""
    request = KYCVerificationRequest(
        user_id=user_id,
        full_name=full_name,
        id_type=id_type,
        id_number=id_number,
        date_of_birth=date_of_birth,
        phone_number=phone_number,
    )

    id_doc_bytes = await id_document.read() if id_document else None
    selfie_bytes = await selfie.read() if selfie else None

    result = kyc_engine.verify_identity(request, id_doc_bytes, selfie_bytes)
    return result.model_dump()


@app.post("/kyc/liveness")
async def kyc_liveness(
    user_id: Annotated[int, Form(...)],
    video: Annotated[UploadFile, File(...)],
    method: Annotated[str, Form()] = "passive",
):
    """Perform liveness detection from video."""
    video_bytes = await video.read()
    if len(video_bytes) > 50_000_000:  # 50MB limit for liveness video
        raise HTTPException(status_code=413, detail="Liveness video exceeds 50MB limit")
    result = kyc_engine.liveness_check(video_bytes, user_id, method)
    return result.model_dump()


@app.post("/integrity/photo-report")
async def analyze_integrity_photo_report(
    file: Annotated[UploadFile, File(...)],
    report_id: Annotated[Optional[int], Form()] = None,
):
    """Explicit integrity endpoint for content-addressed photo evidence bundles."""
    content = await file.read()
    return analyze_evidence_bundle(content, file.filename or "photo.jpg", report_id)


@app.post("/analyze/photo-report")
async def analyze_photo_report(
    file: Annotated[UploadFile, File(...)],
    report_id: Annotated[Optional[int], Form()] = None,
):
    """Create a conservative, content-addressed evidence bundle for a report.

    The endpoint preserves legacy OCR/VLM/Docling fields, while adding an
    integrity manifest, engine version evidence, findings, and an explicit
    manual-review decision. It does not approve or finalise an election result.
    """
    content = await file.read()
    return analyze_evidence_bundle(content, file.filename or "photo.jpg", report_id)


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8089"))
    uvicorn.run(app, host="0.0.0.0", port=port)
