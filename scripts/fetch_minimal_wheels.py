"""Download wheels via curl (pip SSL broken on some hosts) and install offline."""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path
from urllib.request import urlopen

BASE = "https://pypi.tuna.tsinghua.edu.cn"
WHEELS = Path(__file__).resolve().parent.parent / "python" / "wheels"
WHEELS.mkdir(parents=True, exist_ok=True)

# Minimal set to run offline FastAPI agent + heuristics
PACKAGES = [
    "typing-extensions",
    "annotated-types",
    "pydantic-core",
    "pydantic",
    "pydantic-settings",
    "starlette",
    "anyio",
    "idna",
    "sniffio",
    "fastapi",
    "h11",
    "click",
    "colorama",
    "uvicorn",
    "python-dotenv",
    "pypdf",
    "lxml",
    "python-docx",
    "tenacity",
    "certifi",
    "httpcore",
    "httpx",
]


def latest_wheel(pkg: str) -> str | None:
    url = f"{BASE}/simple/{pkg}/"
    html = urlopen(url, timeout=60).read().decode("utf-8", errors="ignore")
    # prefer cp310 win_amd64, then py3-none-any
    patterns = [
        rf'href="(\.\./\.\./packages/[^"]+/{re.escape(pkg.replace("-", "[-_]")}).*?cp310.*?win_amd64\.whl)[^"]*"',
        rf'href="(\.\./\.\./packages/[^"]+/{re.escape(pkg)}-[^"]+-py3-none-any\.whl)[^"]*"',
        rf'href="(\.\./\.\./packages/[^"]+/[^"]*{re.escape(pkg.replace("-", "_"))}[^"]*cp310[^"]*win_amd64\.whl)[^"]*"',
        rf'href="(\.\./\.\./packages/[^"]+/[^"]*py3-none-any\.whl)[^"]*"',
    ]
    # broader: all whl links, pick best
    links = re.findall(r'href="(\.\./\.\./packages/[^"]+\.whl)[^"]*"', html)
    if not links:
        return None
    scored = []
    for rel in links:
        name = rel.split("/")[-1].lower()
        score = 0
        if "win_amd64" in name and "cp310" in name:
            score += 100
        elif "py3-none-any" in name:
            score += 50
        elif "win_amd64" in name:
            score += 40
        # prefer newer by appearing later
        scored.append((score, links.index(rel), rel))
    scored.sort(key=lambda x: (x[0], x[1]), reverse=True)
    best = scored[0][2]
    return best.replace("../../", "/")


def download(rel: str) -> Path:
    url = BASE + rel
    dest = WHEELS / rel.split("/")[-1].split("#")[0]
    if dest.exists() and dest.stat().st_size > 1000:
        print("skip", dest.name)
        return dest
    print("get", url)
    subprocess.check_call(["curl.exe", "-L", "--max-time", "180", "-o", str(dest), url])
    return dest


def main() -> None:
    for pkg in PACKAGES:
        rel = latest_wheel(pkg)
        if not rel:
            print("MISS", pkg)
            continue
        try:
            download(rel)
        except Exception as e:
            print("FAIL", pkg, e)
    print("installing...")
    subprocess.check_call(
        [
            sys.executable,
            "-m",
            "pip",
            "install",
            "--no-index",
            f"--find-links={WHEELS}",
            *PACKAGES,
        ]
    )


if __name__ == "__main__":
    main()
