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


def test_citations_split_official_and_third_party() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={
            "question": "q",
            "answer": "参见 https://www.example.com/guide 与 https://industry.example.org/report，以及 https://example.com/guide。",
            "brand_name": "Acme GEO",
            "brand_domain": "example.com",
        },
    )
    payload = response.json()
    assert len(payload["citations"]) == 2
    official = [item for item in payload["citations"] if item["source_type"] == "official"]
    third_party = [item for item in payload["citations"] if item["source_type"] == "third_party"]
    assert official[0]["domain"] == "example.com"
    assert third_party[0]["domain"] == "industry.example.org"


def test_citation_trailing_punctuation_is_trimmed() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={"question": "q", "answer": "来源：https://example.com/report.", "brand_domain": "example.com"},
    )
    citation = response.json()["citations"][0]
    assert citation["raw_url"] == "https://example.com/report"
    assert citation["source_type"] == "official"


def test_brand_mention_produces_recommendation() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={
            "question": "有哪些 GEO 工具？",
            "answer": "Acme GEO 是值得推荐的 GEO 分析工具，支持提及率与引用分析。",
            "brand_name": "Acme GEO",
            "brand_aliases": ["Acme"],
            "language": "zh-CN",
            "transport_status": "succeeded",
            "finish_reason": "stop",
        },
    )
    payload = response.json()
    assert payload["content_validity"] == "valid"
    assert payload["answer_outcome"] == "recommendation"
    assert payload["recommendation_label"] == "mentioned"
    assert payload["sentiment"] == "positive"
    assert payload["eligible_for_core_metrics"] is True
    assert payload["mentions"][0]["text"] == "Acme GEO"


def test_empty_answer_is_not_eligible() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={"question": "q", "answer": "   ", "brand_name": "Acme GEO"},
    )
    payload = response.json()
    assert payload["content_validity"] == "empty"
    assert payload["answer_outcome"] == "unknown"
    assert payload["eligible_for_core_metrics"] is False


def test_truncated_answer_is_not_eligible() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={
            "question": "q",
            "answer": "Acme GEO is a tool",
            "brand_name": "Acme GEO",
            "finish_reason": "length",
        },
    )
    payload = response.json()
    assert payload["content_validity"] == "truncated"
    assert payload["eligible_for_core_metrics"] is False


def test_refusal_outcome_is_recorded_without_recommendation() -> None:
    response = client.post(
        "/internal/judge/v1/evaluate",
        json={"question": "q", "answer": "抱歉，我无法回答这个问题。", "brand_name": "Acme GEO"},
    )
    payload = response.json()
    assert payload["answer_outcome"] == "refusal"
    assert payload["recommendation_label"] == "none"
    assert payload["eligible_for_core_metrics"] is True

