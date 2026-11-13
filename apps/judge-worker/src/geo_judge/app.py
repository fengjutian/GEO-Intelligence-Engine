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

JUDGE_VERSION = "judge-rules-v1.0"
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
    citations: list[Citation] = []
    seen: set[str] = set()
    for match in URL_PATTERN.finditer(answer):
        raw = match.group(0).rstrip(TRAILING_PUNCTUATION)
        parsed = urlparse(raw)
        domain = parsed.hostname.lower().removeprefix("www.") if parsed.hostname else ""
        if not domain:
            continue
        normalized = urlunparse((parsed.scheme.lower(), _netloc(domain, parsed.port), parsed.path or "/", "", parsed.query, ""))
        if normalized in seen:
            continue
        seen.add(normalized)
        official = bool(brand) and (domain == brand or domain.endswith("." + brand))
        citations.append(
            Citation(
                raw_url=raw,
                normalized_url=normalized,
                domain=domain,
                source_type="official" if official else "third_party",
                confidence=1.0,
            )
        )
    return citations
