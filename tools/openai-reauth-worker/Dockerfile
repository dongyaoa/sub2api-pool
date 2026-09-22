FROM mcr.microsoft.com/playwright:v1.58.2-noble

WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN npm install --global pnpm@10.28.2 && pnpm install --prod --frozen-lockfile --ignore-scripts
COPY src ./src
ENV NODE_ENV=production HOST=127.0.0.1 PORT=8091
USER pwuser
EXPOSE 8091
HEALTHCHECK --interval=30s --timeout=5s CMD node -e "fetch('http://127.0.0.1:8091/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "src/server.mjs"]
