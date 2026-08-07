from __future__ import annotations

import re


_PHONE = re.compile(r"1[3-9]\d{9}")
_EMAIL = re.compile(r"[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}")
_ID = re.compile(r"\b\d{17}[\dXx]\b")


def redact(text: str) -> str:
    if not text:
        return text

    def phone(m: re.Match[str]) -> str:
        s = m.group(0)
        return s[:3] + "****" + s[-4:]

    def email(m: re.Match[str]) -> str:
        s = m.group(0)
        name, _, domain = s.partition("@")
        if not name:
            return "***@" + domain
        return name[0] + "***@" + domain

    text = _PHONE.sub(phone, text)
    text = _EMAIL.sub(email, text)
    text = _ID.sub("******************", text)
    return text
