"""Production biometric quality assessment engine.

NFIQ2-compatible fingerprint quality, ICAO face quality,
ISO/IEC 29794-6 iris quality assessment.
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from enum import Enum

import cv2
import numpy as np


class QualityLevel(str, Enum):
    EXCELLENT = "excellent"
    GOOD = "good"
    FAIR = "fair"
    POOR = "poor"
    REJECT = "reject"


@dataclass
class QualityReport:
    overall_score: float
    level: QualityLevel
    metrics: dict
    pass_threshold: bool
    rejection_reasons: list[str] = field(default_factory=list)
    processing_time_ms: float = 0.0


class FingerprintQualityAssessor:
    """NFIQ2-compatible fingerprint quality assessment."""

    THRESHOLDS = {
        QualityLevel.EXCELLENT: 0.85,
        QualityLevel.GOOD: 0.70,
        QualityLevel.FAIR: 0.50,
        QualityLevel.POOR: 0.30,
    }
    ENROLLMENT_THRESHOLD = 0.50

    def assess(self, image: np.ndarray) -> QualityReport:
        start = time.monotonic()

        gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY) if len(image.shape) == 3 else image
        h, w = gray.shape

        metrics = {}
        reasons = []

        # Foreground/background segmentation quality
        fg_ratio = self._foreground_ratio(gray)
        metrics["foreground_ratio"] = round(fg_ratio, 4)
        if fg_ratio < 0.3:
            reasons.append("insufficient_foreground")

        # Ridge-valley contrast
        contrast = self._ridge_valley_contrast(gray)
        metrics["ridge_valley_contrast"] = round(contrast, 4)
        if contrast < 0.15:
            reasons.append("low_contrast")

        # Ridge orientation certainty
        certainty = self._orientation_certainty(gray)
        metrics["orientation_certainty"] = round(certainty, 4)
        if certainty < 0.3:
            reasons.append("uncertain_orientation")

        # Ridge frequency uniformity
        freq_uniformity = self._frequency_uniformity(gray)
        metrics["frequency_uniformity"] = round(freq_uniformity, 4)

        # Sharpness (Laplacian variance)
        laplacian = cv2.Laplacian(gray, cv2.CV_64F)
        sharpness = min(float(np.var(laplacian)) / 800.0, 1.0)
        metrics["sharpness"] = round(sharpness, 4)
        if sharpness < 0.15:
            reasons.append("blurry")

        # Resolution check
        if w < 200 or h < 200:
            reasons.append("low_resolution")
        resolution_score = min(min(w, h) / 300.0, 1.0)
        metrics["resolution_score"] = round(resolution_score, 4)

        # Moisture/dryness
        moisture = self._moisture_score(gray)
        metrics["moisture_score"] = round(moisture, 4)
        if moisture < 0.2:
            reasons.append("too_dry")
        elif moisture > 0.9:
            reasons.append("too_wet")

        # Overall NFIQ2-like score
        overall = (
            fg_ratio * 0.15
            + contrast * 0.20
            + certainty * 0.20
            + freq_uniformity * 0.10
            + sharpness * 0.15
            + resolution_score * 0.10
            + moisture * 0.10
        )

        level = QualityLevel.REJECT
        for lv, thresh in sorted(self.THRESHOLDS.items(), key=lambda x: x[1], reverse=True):
            if overall >= thresh:
                level = lv
                break

        nfiq2 = max(1, min(5, int(round(overall * 4 + 1))))
        metrics["nfiq2_score"] = nfiq2

        return QualityReport(
            overall_score=round(overall, 4),
            level=level,
            metrics=metrics,
            pass_threshold=overall >= self.ENROLLMENT_THRESHOLD,
            rejection_reasons=reasons,
            processing_time_ms=round((time.monotonic() - start) * 1000, 2),
        )

    def _foreground_ratio(self, gray: np.ndarray) -> float:
        bs = 16
        h, w = gray.shape
        fg_blocks = 0
        total_blocks = 0

        for i in range(0, h - bs, bs):
            for j in range(0, w - bs, bs):
                block = gray[i:i+bs, j:j+bs].astype(np.float64)
                if np.var(block) > 100:
                    fg_blocks += 1
                total_blocks += 1

        return fg_blocks / max(total_blocks, 1)

    def _ridge_valley_contrast(self, gray: np.ndarray) -> float:
        bs = 32
        h, w = gray.shape
        contrasts = []

        for i in range(0, h - bs, bs):
            for j in range(0, w - bs, bs):
                block = gray[i:i+bs, j:j+bs].astype(np.float64)
                if np.var(block) < 50:
                    continue
                projection = np.mean(block, axis=0)
                if len(projection) > 1:
                    local_contrast = (np.max(projection) - np.min(projection)) / 255.0
                    contrasts.append(local_contrast)

        return float(np.mean(contrasts)) if contrasts else 0.0

    def _orientation_certainty(self, gray: np.ndarray) -> float:
        sobelx = cv2.Sobel(gray.astype(np.float64), cv2.CV_64F, 1, 0, ksize=3)
        sobely = cv2.Sobel(gray.astype(np.float64), cv2.CV_64F, 0, 1, ksize=3)

        bs = 16
        h, w = gray.shape
        certainties = []

        for i in range(0, h - bs, bs):
            for j in range(0, w - bs, bs):
                gx = sobelx[i:i+bs, j:j+bs]
                gy = sobely[i:i+bs, j:j+bs]

                gxx = np.sum(gx ** 2)
                gyy = np.sum(gy ** 2)
                gxy = np.sum(gx * gy)

                denom = gxx + gyy
                if denom < 1e-6:
                    continue

                coherence = np.sqrt((gxx - gyy) ** 2 + 4 * gxy ** 2) / denom
                certainties.append(coherence)

        return float(np.mean(certainties)) if certainties else 0.0

    def _frequency_uniformity(self, gray: np.ndarray) -> float:
        bs = 32
        h, w = gray.shape
        frequencies = []

        for i in range(0, h - bs, bs):
            for j in range(0, w - bs, bs):
                block = gray[i:i+bs, j:j+bs].astype(np.float64)
                if np.var(block) < 50:
                    continue
                projection = np.mean(block, axis=0)
                diffs = np.diff(np.sign(np.diff(projection)))
                peaks = np.sum(diffs == -2)
                if peaks > 0:
                    freq = peaks / len(projection)
                    frequencies.append(freq)

        if len(frequencies) < 3:
            return 0.0

        std = np.std(frequencies)
        mean = np.mean(frequencies)
        cv = std / max(mean, 1e-6)
        return max(0.0, 1.0 - cv)

    def _moisture_score(self, gray: np.ndarray) -> float:
        hist = cv2.calcHist([gray], [0], None, [256], [0, 256]).flatten()
        hist = hist / max(hist.sum(), 1)

        low = np.sum(hist[:60])
        mid = np.sum(hist[60:200])
        high = np.sum(hist[200:])

        if mid > 0.6:
            return 0.7
        elif low > 0.4:
            return 0.2  # too dry
        elif high > 0.3:
            return 0.9  # too wet
        return 0.5


class FaceQualityAssessor:
    """ICAO/ISO 19794-5 face quality assessment."""

    ENROLLMENT_THRESHOLD = 0.55

    def assess(self, image: np.ndarray, face_bbox: tuple[int, int, int, int] | None = None) -> QualityReport:
        start = time.monotonic()
        metrics = {}
        reasons = []

        if face_bbox:
            x1, y1, x2, y2 = face_bbox
            face = image[y1:y2, x1:x2]
        else:
            face = image

        if face.size == 0:
            return QualityReport(
                overall_score=0.0, level=QualityLevel.REJECT, metrics={},
                pass_threshold=False, rejection_reasons=["no_face"],
                processing_time_ms=0.0,
            )

        gray = cv2.cvtColor(face, cv2.COLOR_BGR2GRAY) if len(face.shape) == 3 else face
        h, w = gray.shape

        brightness = float(np.mean(gray)) / 255.0
        metrics["brightness"] = round(brightness, 4)
        if brightness < 0.15 or brightness > 0.90:
            reasons.append("brightness_out_of_range")

        contrast = float(np.std(gray)) / 128.0
        metrics["contrast"] = round(min(contrast, 1.0), 4)
        if contrast < 0.1:
            reasons.append("low_contrast")

        lap = cv2.Laplacian(gray, cv2.CV_64F)
        sharpness = min(float(np.var(lap)) / 500.0, 1.0)
        metrics["sharpness"] = round(sharpness, 4)
        if sharpness < 0.1:
            reasons.append("blurry")

        resolution = min(min(w, h) / 200.0, 1.0)
        metrics["resolution_score"] = round(resolution, 4)
        if min(w, h) < 80:
            reasons.append("low_resolution")

        if len(face.shape) == 3:
            hsv = cv2.cvtColor(face, cv2.COLOR_BGR2HSV)
            saturation = float(np.mean(hsv[:, :, 1])) / 255.0
            metrics["saturation"] = round(saturation, 4)
        else:
            saturation = 0.5

        left_half = gray[:, :w//2]
        right_half = gray[:, w//2:]
        symmetry = 1.0 - min(abs(float(np.mean(left_half)) - float(np.mean(right_half))) / 50.0, 1.0)
        metrics["illumination_symmetry"] = round(symmetry, 4)
        if symmetry < 0.5:
            reasons.append("uneven_illumination")

        aspect_ratio = w / max(h, 1)
        face_ratio_score = 1.0 - min(abs(aspect_ratio - 0.75) / 0.5, 1.0)
        metrics["face_aspect_ratio"] = round(aspect_ratio, 4)

        overall = (
            min(max(brightness, 0), 1) * 0.10
            + min(contrast, 1.0) * 0.15
            + sharpness * 0.20
            + resolution * 0.15
            + saturation * 0.05
            + symmetry * 0.15
            + face_ratio_score * 0.10
            + (1.0 if not reasons else 0.5) * 0.10
        )

        level = QualityLevel.REJECT
        for lv, thresh in [
            (QualityLevel.EXCELLENT, 0.85),
            (QualityLevel.GOOD, 0.70),
            (QualityLevel.FAIR, 0.50),
            (QualityLevel.POOR, 0.30),
        ]:
            if overall >= thresh:
                level = lv
                break

        return QualityReport(
            overall_score=round(overall, 4),
            level=level,
            metrics=metrics,
            pass_threshold=overall >= self.ENROLLMENT_THRESHOLD,
            rejection_reasons=reasons,
            processing_time_ms=round((time.monotonic() - start) * 1000, 2),
        )


class IrisQualityAssessor:
    """ISO/IEC 29794-6 iris quality assessment."""

    ENROLLMENT_THRESHOLD = 0.50

    def assess(self, image: np.ndarray) -> QualityReport:
        start = time.monotonic()
        metrics = {}
        reasons = []

        gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY) if len(image.shape) == 3 else image
        h, w = gray.shape

        focus = min(float(np.var(cv2.Laplacian(gray, cv2.CV_64F))) / 300.0, 1.0)
        metrics["focus_score"] = round(focus, 4)
        if focus < 0.2:
            reasons.append("out_of_focus")

        margin = min(w, h) // 4
        center = gray[margin:h-margin, margin:w-margin]
        usable = np.sum(center > 30) / max(center.size, 1)
        metrics["usable_iris_area"] = round(usable, 4)
        if usable < 0.5:
            reasons.append("insufficient_iris")

        brightness = float(np.mean(gray)) / 255.0
        metrics["brightness"] = round(brightness, 4)
        if brightness < 0.1 or brightness > 0.8:
            reasons.append("brightness_out_of_range")

        contrast = float(np.std(gray)) / 128.0
        metrics["contrast"] = round(min(contrast, 1.0), 4)

        resolution = min(min(w, h) / 200.0, 1.0)
        metrics["resolution_score"] = round(resolution, 4)
        if min(w, h) < 100:
            reasons.append("low_resolution")

        gaze_offset = self._estimate_gaze_offset(gray)
        metrics["gaze_offset"] = round(gaze_offset, 4)
        if gaze_offset > 0.3:
            reasons.append("off_axis_gaze")

        overall = (
            focus * 0.25
            + usable * 0.25
            + min(max(brightness, 0), 1) * 0.10
            + min(contrast, 1.0) * 0.15
            + resolution * 0.15
            + (1.0 - gaze_offset) * 0.10
        )

        level = QualityLevel.REJECT
        for lv, thresh in [
            (QualityLevel.EXCELLENT, 0.85),
            (QualityLevel.GOOD, 0.70),
            (QualityLevel.FAIR, 0.50),
            (QualityLevel.POOR, 0.30),
        ]:
            if overall >= thresh:
                level = lv
                break

        return QualityReport(
            overall_score=round(overall, 4),
            level=level,
            metrics=metrics,
            pass_threshold=overall >= self.ENROLLMENT_THRESHOLD,
            rejection_reasons=reasons,
            processing_time_ms=round((time.monotonic() - start) * 1000, 2),
        )

    def _estimate_gaze_offset(self, gray: np.ndarray) -> float:
        h, w = gray.shape
        dark_mask = gray < 50
        if np.sum(dark_mask) == 0:
            return 0.5

        ys, xs = np.where(dark_mask)
        pupil_cy = np.mean(ys) / h
        pupil_cx = np.mean(xs) / w

        offset = np.sqrt((pupil_cx - 0.5) ** 2 + (pupil_cy - 0.5) ** 2)
        return min(offset * 2, 1.0)
