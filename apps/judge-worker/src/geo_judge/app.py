from fastapi import FastAPI

from .models import ContentValidity, EligibilityInput, EligibilityResult, TransportStatus

app = FastAPI(title="GEO Answer Judge", version="1.0.0")


@app.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/internal/judge/v1/eligibility", response_model=EligibilityResult)
def eligibility(value: EligibilityInput) -> EligibilityResult:
    eligible = (
        value.transport_status == TransportStatus.SUCCEEDED
        and value.content_validity == ContentValidity.VALID
    )
    return EligibilityResult(eligible_for_core_metrics=eligible)
