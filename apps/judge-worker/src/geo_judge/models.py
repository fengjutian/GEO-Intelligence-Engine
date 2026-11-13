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
