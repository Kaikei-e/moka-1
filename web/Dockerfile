# syntax=docker/dockerfile:1
# moka-web: SvelteKit SSR (adapter-node)
FROM node:26.4.0-trixie-slim AS build
# Node 26 は corepack を同梱しないので pnpm は明示インストール(バージョン pin)
RUN npm install -g pnpm@11.9.0
WORKDIR /app
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY . .
RUN pnpm run build && pnpm prune --prod

FROM node:26.4.0-trixie-slim
ENV NODE_ENV=production PORT=3000
WORKDIR /app
COPY --from=build /app/build ./build
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json ./
EXPOSE 3000
USER node
CMD ["node", "build"]
