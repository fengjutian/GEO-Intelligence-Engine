FROM python:3.13-slim
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY apps/judge-worker/pyproject.toml ./
COPY apps/judge-worker/src ./src
RUN uv pip install --system .
ENV PYTHONPATH=/app/src
USER 65532:65532
EXPOSE 8081
CMD ["uvicorn","geo_judge.app:app","--host","0.0.0.0","--port","8081"]
