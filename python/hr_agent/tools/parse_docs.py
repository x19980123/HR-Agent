from __future__ import annotations

import io
import logging
import os
from pathlib import Path

log = logging.getLogger("hr_agent.parse_docs")

# Limit OCR cost/latency on long scanned resumes.
_MAX_OCR_PAGES = 8
_OCR_ZOOM = 2.0  # ~144–150 DPI equivalent for rasterization


def resolve_resume_path(path: str) -> Path:
    """Find resume on disk; tolerate relative paths saved from a different CWD."""
    raw = (path or "").strip()
    if not raw:
        return Path("")
    p = Path(raw)
    if p.is_file():
        return p
    # Windows path stored with mixed separators
    norm = Path(raw.replace("\\", "/"))
    if norm.is_file():
        return norm
    base = norm.name or p.name
    if not base:
        return p

    candidates: list[Path] = []
    upload = (os.environ.get("UPLOAD_DIR") or "").strip()
    if upload:
        candidates.append(Path(upload) / base)
        candidates.append(Path(upload) / norm.as_posix().lstrip("./"))
    # repo root: python/hr_agent/tools/parse_docs.py -> parents[3]
    try:
        repo = Path(__file__).resolve().parents[3]
        candidates.extend(
            [
                repo / "uploads" / base,
                repo / "services" / "uploads" / base,
                repo / "python" / "uploads" / base,
            ]
        )
    except IndexError:
        pass
    cwd = Path.cwd()
    candidates.extend(
        [
            cwd / raw,
            cwd / norm.as_posix(),
            cwd / "uploads" / base,
            cwd / "services" / "uploads" / base,
            cwd.parent / "uploads" / base,
            cwd.parent / "services" / "uploads" / base,
        ]
    )
    seen: set[str] = set()
    for c in candidates:
        key = str(c.resolve()) if c.exists() else str(c)
        if key in seen:
            continue
        seen.add(key)
        if c.is_file():
            log.info("resolved resume path %s -> %s", path, c)
            return c
    log.warning("resume file not found: %s (cwd=%s upload_dir=%s)", path, cwd, upload)
    return p


def extract_text(path: str, fallback_text: str = "") -> str:
    if fallback_text and fallback_text.strip():
        return fallback_text.strip()
    p = resolve_resume_path(path)
    if not p.exists():
        return fallback_text or ""
    suffix = p.suffix.lower()
    if suffix in {".txt", ".md", ""}:
        return p.read_text(encoding="utf-8", errors="ignore")
    if suffix == ".pdf":
        return _pdf(p)
    if suffix in {".docx", ".doc"}:
        return _docx(p)
    if suffix in {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".tif", ".tiff"}:
        return try_ocr_image(p)
    return p.read_text(encoding="utf-8", errors="ignore")


def _pdf(p: Path) -> str:
    text = _pdf_text_layer(p)
    if text.strip():
        return text.strip()
    log.info("PDF text layer empty, running OCR: %s", p)
    ocr = try_ocr_pdf(p)
    return (ocr or "").strip()


def _pdf_text_layer(p: Path) -> str:
    try:
        from pypdf import PdfReader
    except ImportError:
        return p.read_bytes().decode("utf-8", errors="ignore")
    try:
        reader = PdfReader(str(p))
        parts: list[str] = []
        for page in reader.pages:
            parts.append(page.extract_text() or "")
        return "\n".join(parts).strip()
    except Exception as e:
        log.warning("pypdf extract failed for %s: %s", p, e)
        return ""


def _docx(p: Path) -> str:
    try:
        from docx import Document
    except ImportError:
        return p.read_bytes().decode("utf-8", errors="ignore")
    doc = Document(str(p))
    text = "\n".join(para.text for para in doc.paragraphs).strip()
    if text:
        return text
    # Some docx are image-only; no reliable embedded OCR without unpacking.
    return ""


_rapid_ocr = None


def _get_rapid_ocr():
    global _rapid_ocr
    if _rapid_ocr is not None:
        return _rapid_ocr
    try:
        from rapidocr_onnxruntime import RapidOCR
    except ImportError:
        log.warning("rapidocr-onnxruntime not installed; OCR unavailable")
        return None
    _rapid_ocr = RapidOCR()
    return _rapid_ocr


def _ocr_numpy(img) -> str:
    engine = _get_rapid_ocr()
    if engine is None:
        return ""
    try:
        result, _ = engine(img)
    except Exception as e:
        log.warning("RapidOCR failed: %s", e)
        return ""
    if not result:
        return ""
    # result items: [box, text, score]
    lines: list[str] = []
    for item in result:
        if not item or len(item) < 2:
            continue
        text = str(item[1]).strip()
        if text:
            lines.append(text)
    return "\n".join(lines).strip()


def try_ocr_pdf(p: Path) -> str:
    """Rasterize PDF pages with PyMuPDF, then OCR with RapidOCR (onnx)."""
    try:
        import fitz  # PyMuPDF
        import numpy as np
        from PIL import Image
    except ImportError as e:
        log.warning("OCR deps missing (pymupdf/Pillow): %s", e)
        return _try_ocr_pdf_tesseract(p)

    if _get_rapid_ocr() is None:
        return _try_ocr_pdf_tesseract(p)

    try:
        doc = fitz.open(str(p))
    except Exception as e:
        log.warning("open PDF for OCR failed: %s", e)
        return ""

    chunks: list[str] = []
    try:
        page_count = min(len(doc), _MAX_OCR_PAGES)
        mat = fitz.Matrix(_OCR_ZOOM, _OCR_ZOOM)
        for i in range(page_count):
            page = doc.load_page(i)
            pix = page.get_pixmap(matrix=mat, alpha=False)
            img = Image.open(io.BytesIO(pix.tobytes("png"))).convert("RGB")
            text = _ocr_numpy(np.array(img))
            if text:
                chunks.append(text)
    finally:
        doc.close()

    joined = "\n\n".join(chunks).strip()
    if joined:
        log.info("OCR extracted %d chars from %s", len(joined), p.name)
    else:
        log.warning("OCR produced empty text for %s", p)
    return joined


def try_ocr_image(p: Path) -> str:
    try:
        import numpy as np
        from PIL import Image
    except ImportError:
        return ""
    try:
        img = Image.open(p).convert("RGB")
        return _ocr_numpy(np.array(img))
    except Exception as e:
        log.warning("image OCR failed for %s: %s", p, e)
        return ""


def _try_ocr_pdf_tesseract(p: Path) -> str:
    """Fallback if RapidOCR stack unavailable but Tesseract is present."""
    try:
        from pdf2image import convert_from_path  # type: ignore
        import pytesseract  # type: ignore
    except ImportError:
        return ""
    try:
        images = convert_from_path(str(p), dpi=200)
        chunks: list[str] = []
        for img in images[:_MAX_OCR_PAGES]:
            chunks.append(pytesseract.image_to_string(img, lang="chi_sim+eng") or "")
        return "\n".join(chunks).strip()
    except Exception as e:
        log.warning("tesseract OCR fallback failed: %s", e)
        return ""


def ocr_stub(path: str) -> str:
    """Used by heuristic retry: run real OCR, else return a detectable stub marker."""
    if not path:
        return "[ocr_stub] empty path"
    p = Path(path)
    if not p.exists():
        return f"[ocr_stub] missing file {path}"
    suffix = p.suffix.lower()
    if suffix == ".pdf":
        got = try_ocr_pdf(p)
    elif suffix in {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".tif", ".tiff"}:
        got = try_ocr_image(p)
    else:
        got = ""
    if got.strip():
        return got.strip()
    return f"[ocr_stub] unable to OCR locally for {path}; use clearer PDF/DOCX text extract"
