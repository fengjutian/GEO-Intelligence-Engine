import math
import re
from urllib.parse import urlparse, urlunparse

from fastapi import FastAPI

from .models import (
    AnswerOutcome,
    Citation,
    ContentValidity,
    EligibilityInput,
    EligibilityResult,
    JudgeRequest,
    JudgeResult,
    Mention,
    TransportStatus,
)

app = FastAPI(title="GEO Answer Judge", version="1.0.0")

JUDGE_VERSION = "judge-rules-v1.1"
ELIGIBILITY_RULE_VERSION = "eligibility-v1.0"

REFUSAL_MARKERS = (
    "无法回答",
    "不能提供",
    "无法提供",
    "抱歉",
    "i can't",
    "i cannot",
    "cannot assist",
    "unable to answer",
)
POSITIVE_MARKERS = ("推荐", "最佳", "领先", "recommend", "best", "leading")
URL_PATTERN = re.compile(r"https?://[^\s<>\"')\]}]+", re.IGNORECASE)
TEXT_UNIT_PATTERN = re.compile(r"[A-Za-z0-9]+(?:['’-][A-Za-z0-9]+)*|[\u3400-\u4dbf\u4e00-\u9fff]")
TRAILING_PUNCTUATION = ".,;:!?。，、；：！？）】》"


@app.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/internal/judge/v1/eligibility", response_model=EligibilityResult)
def eligibility(value: EligibilityInput) -> EligibilityResult:
    eligible = (
        value.transport_status == TransportStatus.SUCCEEDED
        and value.content_validity == ContentValidity.VALID
    )
    return EligibilityResult(eligible_for_core_metrics=eligible, eligibility_rule_version=ELIGIBILITY_RULE_VERSION)


@app.post("/internal/judge/v1/evaluate", response_model=JudgeResult)
def evaluate(value: JudgeRequest) -> JudgeResult:
    answer = value.answer.strip()
    lowered = answer.lower()

    if not answer:
        validity = ContentValidity.EMPTY
    elif value.finish_reason == "length":
        validity = ContentValidity.TRUNCATED
    else:
        validity = ContentValidity.VALID

    mentions = find_mentions(answer, value.brand_name, value.brand_aliases)
    if any(marker in lowered for marker in REFUSAL_MARKERS):
        outcome = AnswerOutcome.REFUSAL
    elif mentions:
        outcome = AnswerOutcome.RECOMMENDATION
    elif answer:
        outcome = AnswerOutcome.NO_RECOMMENDATION
    else:
        outcome = AnswerOutcome.UNKNOWN

    if not mentions:
        recommendation = "not_mentioned"
    else:
        recommendation = "mentioned"
    if outcome == AnswerOutcome.REFUSAL:
        recommendation = "none"

    sentiment = "neutral"
    if mentions and any(marker in lowered for marker in POSITIVE_MARKERS):
        sentiment = "positive"

    eligible = value.transport_status == TransportStatus.SUCCEEDED and validity == ContentValidity.VALID
    citations = extract_citations(answer, value.brand_domain)
    return JudgeResult(
        content_validity=validity,
        answer_outcome=outcome,
        recommendation_label=recommendation,
        sentiment=sentiment,
        confidence=1.0,
        eligible_for_core_metrics=eligible,
        judge_version=JUDGE_VERSION,
        eligibility_rule_version=ELIGIBILITY_RULE_VERSION,
        mentions=mentions,
        citations=citations,
    )


def find_mentions(answer: str, brand_name: str, aliases: list[str]) -> list[Mention]:
    lowered = answer.lower()
    candidates = [brand_name, *aliases]
    mentions: list[Mention] = []
    seen: set[str] = set()
    for candidate in candidates:
        candidate = candidate.strip()
        if not candidate or candidate.lower() in seen:
            continue
        seen.add(candidate.lower())
        start = lowered.find(candidate.lower())
        if start >= 0:
            mentions.append(Mention(text=answer[start : start + len(candidate)], start=start, end=start + len(candidate)))
    return mentions


def _netloc(domain: str, port: int | None) -> str:
    return f"{domain}:{port}" if port else domain


def extract_citations(answer: str, brand_domain: str) -> list[Citation]:
    brand = brand_domain.strip().lower().removeprefix("www.")
    sentence_spans = _sentence_spans(answer)
    answer_word_count = _word_count(URL_PATTERN.sub(" ", answer))
    occurrences: list[dict[str, object]] = []
    for match in URL_PATTERN.finditer(answer):
        raw = match.group(0).rstrip(TRAILING_PUNCTUATION)
        parsed = urlparse(raw)
        domain = parsed.hostname.lower().removeprefix("www.") if parsed.hostname else ""
        if not domain:
            continue
        normalized = urlunparse((parsed.scheme.lower(), _netloc(domain, parsed.port), parsed.path or "/", "", parsed.query, ""))
        sentence_index = next(
            (index for index, (start, end, _) in enumerate(sentence_spans) if start <= match.start() < end),
            max(len(sentence_spans) - 1, 0),
        )
        occurrences.append({"raw": raw, "normalized": normalized, "domain": domain, "sentence_index": sentence_index})

    unique_per_sentence: dict[int, int] = {}
    for sentence_index in range(len(sentence_spans)):
        unique_per_sentence[sentence_index] = len(
            {item["normalized"] for item in occurrences if item["sentence_index"] == sentence_index}
        )
    grouped: dict[str, dict[str, object]] = {}
    for item in occurrences:
        normalized = str(item["normalized"])
        sentence_index = int(item["sentence_index"])
        sentence_text = sentence_spans[sentence_index][2]
        sentence_words = _word_count(URL_PATTERN.sub(" ", sentence_text))
        credited_words = sentence_words / max(unique_per_sentence[sentence_index], 1)
        entry = grouped.setdefault(
            normalized,
            {
                **item,
                "first_sentence_index": sentence_index,
                "citation_word_count": 0.0,
                "position_adjusted_word_count": 0.0,
                "credited_sentences": set(),
            },
        )
        credited_sentences = entry["credited_sentences"]
        if sentence_index in credited_sentences:
            continue
        credited_sentences.add(sentence_index)
        entry["first_sentence_index"] = min(int(entry["first_sentence_index"]), sentence_index)
        entry["citation_word_count"] = float(entry["citation_word_count"]) + credited_words
        position_weight = math.exp(-sentence_index / max(len(sentence_spans), 1))
        entry["position_adjusted_word_count"] = float(entry["position_adjusted_word_count"]) + credited_words * position_weight

    citations: list[Citation] = []
    for entry in grouped.values():
        domain = str(entry["domain"])
        official = bool(brand) and (domain == brand or domain.endswith("." + brand))
        citation_words = float(entry["citation_word_count"])
        adjusted_words = float(entry["position_adjusted_word_count"])
        citations.append(
            Citation(
                raw_url=str(entry["raw"]),
                normalized_url=str(entry["normalized"]),
                domain=domain,
                source_type="official" if official else "third_party",
                confidence=1.0,
                first_citation_sentence_index=int(entry["first_sentence_index"]),
                sentence_count=len(sentence_spans),
                citation_word_count=citation_words,
                answer_word_count=answer_word_count,
                citation_word_share=citation_words / answer_word_count if answer_word_count else 0.0,
                position_adjusted_word_share=adjusted_words / answer_word_count if answer_word_count else 0.0,
            )
        )
    return citations


def _word_count(text: str) -> int:
    """Count Latin word groups and individual CJK characters as comparable text units."""
    return len(TEXT_UNIT_PATTERN.findall(text))


def _sentence_spans(text: str) -> list[tuple[int, int, str]]:
    """Split sentences without treating punctuation inside URLs as boundaries."""
    url_ranges = [
        (match.start(), match.start() + len(match.group(0).rstrip(TRAILING_PUNCTUATION)))
        for match in URL_PATTERN.finditer(text)
    ]
    spans: list[tuple[int, int, str]] = []
    start = 0
    index = 0
    while index < len(text):
        in_url = any(url_start <= index < url_end for url_start, url_end in url_ranges)
        if not in_url and text[index] in ".!?。！？\r\n":
            end = index + 1
            while end < len(text) and text[end] in ".!?。！？\r\n":
                end += 1
            if text[start:end].strip():
                spans.append((start, end, text[start:end]))
            start = end
            index = end
            continue
        index += 1
    if text[start:].strip():
        spans.append((start, len(text), text[start:]))
    return spans
