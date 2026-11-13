FROM node:24-alpine AS build
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/crawler-worker/package.json apps/crawler-worker/package.json
RUN pnpm install --frozen-lockfile --filter @geo/crawler-worker...
COPY apps/crawler-worker apps/crawler-worker
RUN pnpm --filter @geo/crawler-worker build
FROM node:24-alpine
USER node
WORKDIR /app
COPY --from=build /src/apps/crawler-worker/dist ./dist
CMD ["node","dist/index.js"]
