from fastapi.testclient import TestClient

from geo_judge.app import app

client = TestClient(app)


def test_health() -> None:
    assert client.get("/healthz").json() == {"status": "ok"}


def test_refusal_is_eligible_when_transport_and_content_are_valid() -> None:
    response = client.post(
        "/internal/judge/v1/eligibility",
        json={
            "transport_status": "succeeded",
            "content_validity": "valid",
            "answer_outcome": "refusal",
        },
    )
    assert response.status_code == 200
    assert response.json()["eligible_for_core_metrics"] is True


def test_failed_transport_is_not_eligible() -> None:
    response = client.post(
        "/internal/judge/v1/eligibility",
        json={
            "transport_status": "timeout",
            "content_validity": "valid",
            "answer_outcome": "unknown",
        },
    )
    assert response.json()["eligible_for_core_metrics"] is False
