import re
from fastapi import FastAPI
from pydantic import BaseModel
import spacy

app = FastAPI()
nlp = spacy.load("en_core_web_sm")  # loaded once at startup

class DetectRequest(BaseModel):
    text: str

SSN_RE = re.compile(r"\b\d{3}-\d{2}-\d{4}\b")
EMAIL_RE = re.compile(r"\b[\w.+-]+@[\w-]+\.[\w.-]+\b")
PHONE_RE = re.compile(r"\b(?:\+?\d{1,3}[\s-]?)?(?:\(?\d{3}\)?[\s-]?)\d{3}[\s-]?\d{4}\b")
CARD_RE = re.compile(r"\b(?:\d[ -]?){13,19}\b")

SPACY_MAP = {"PERSON": "NAME", "GPE": "LOCATION", "LOC": "LOCATION", "ORG": "ORG"}

def luhn_ok(number: str) -> bool:
    digits = [int(c) for c in number if c.isdigit()]
    if not 13 <= len(digits) <= 19:
        return False
    total = 0
    for i, d in enumerate(reversed(digits)):
        if i % 2 == 1:
            d *= 2
            if d > 9:
                d -= 9
        total += d
    return total % 10 == 0

def overlaps(a, b):
    return a["start"] < b["end"] and b["start"] < a["end"]

@app.post("/detect")
def detect(req: DetectRequest):
    text = req.text
    found = []

    def add(type_, m_start, m_end):
        item = {"type": type_, "start": m_start, "end": m_end, "value": text[m_start:m_end]}
        if not any(overlaps(item, f) for f in found):  # rules win over NLP
            found.append(item)

    # 1. Rule-based layer (highest priority)
    for m in SSN_RE.finditer(text):
        add("SSN", m.start(), m.end())
    for m in CARD_RE.finditer(text):
        if luhn_ok(m.group()):
            add("CREDIT_CARD", m.start(), m.end())
    for m in EMAIL_RE.finditer(text):
        add("EMAIL", m.start(), m.end())
    for m in PHONE_RE.finditer(text):
        add("PHONE", m.start(), m.end())

    # 2. NLP layer (names, places, orgs)
    for ent in nlp(text).ents:
        if ent.label_ in SPACY_MAP:
            add(SPACY_MAP[ent.label_], ent.start_char, ent.end_char)

    return sorted(found, key=lambda x: x["start"])