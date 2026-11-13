FROM mcr.microsoft.com/playwright:v1.65.0-noble
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/crawler-worker/package.json apps/crawler-worker/package.json
RUN pnpm install --frozen-lockfile --filter @geo/crawler-worker...
COPY apps/crawler-worker apps/crawler-worker
RUN pnpm --filter @geo/crawler-worker build
WORKDIR /src/apps/crawler-worker
USER pwuser
EXPOSE 8082
CMD ["node","dist/index.js"]
