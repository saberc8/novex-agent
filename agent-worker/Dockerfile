FROM node:24.22.2-bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ripgrep && rm -rf /var/lib/apt/lists/*
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund
COPY src ./src
COPY skills ./skills
USER node
ENTRYPOINT []
CMD ["node", "/app/src/execute.js"]
