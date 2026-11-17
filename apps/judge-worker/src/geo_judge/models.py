from enum import StrEnum

from pydantic import BaseModel, Field


class TransportStatus(StrEnum):
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    TIMEOUT = "timeout"
    CANCELLED = "cancelled"
    OUTCOME_UNKNOWN = "outcome_unknown"


class ContentValidity(StrEnum):
    VALID = "valid"
    EMPTY = "empty"
    TRUNCATED = "truncated"
    WRONG_LANGUAGE = "wrong_language"
    POLICY_BLOCKED = "policy_blocked"
    UNPARSEABLE = "unparseable"


class AnswerOutcome(StrEnum):
    ANSWERED = "answered"
    REFUSAL = "refusal"
    NO_RECOMMENDATION = "no_recommendation"
    RECOMMENDATION = "recommendation"
    UNKNOWN = "unknown"


class EligibilityInput(BaseModel):
    transport_status: TransportStatus
    content_validity: ContentValidity
    answer_outcome: AnswerOutcome


class EligibilityResult(BaseModel):
    eligible_for_core_metrics: bool
    eligibility_rule_version: str = Field(default="eligibility-v1.0")


class JudgeRequest(BaseModel):
    question: str
    answer: str
    brand_name: str = ""
    brand_domain: str = ""
    brand_aliases: list[str] = Field(default_factory=list)
    language: str = ""
    transport_status: TransportStatus = TransportStatus.SUCCEEDED
    finish_reason: str = ""


class Mention(BaseModel):
    text: str
    start: int
    end: int


class Citation(BaseModel):
    raw_url: str
    normalized_url: str
    domain: str
    source_type: str
    confidence: float
    first_citation_sentence_index: int
    sentence_count: int
    citation_word_count: float
    answer_word_count: int
    citation_word_share: float
    position_adjusted_word_share: float


class JudgeResult(BaseModel):
    content_validity: ContentValidity
    answer_outcome: AnswerOutcome
    recommendation_label: str
    sentiment: str
    confidence: float
    eligible_for_core_metrics: bool
    judge_version: str
    eligibility_rule_version: str
    mentions: list[Mention] = Field(default_factory=list)
    citations: list[Citation] = Field(default_factory=list)

