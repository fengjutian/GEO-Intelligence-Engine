FROM node:24-alpine AS build
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --frozen-lockfile --filter @geo/web...
COPY apps/web apps/web
RUN pnpm --filter @geo/web build
FROM nginx:1.29-alpine
COPY --from=build /src/apps/web/dist /usr/share/nginx/html
COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf
