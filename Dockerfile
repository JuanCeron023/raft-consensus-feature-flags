# --- Frontend build stage (React + TypeScript + Vite) ---------------------
FROM node:20-alpine AS frontend
WORKDIR /fe
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build   # outputs to /web/static (see vite.config.ts outDir)

# --- Go build stage --------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
# Bring in the compiled frontend so it can be embedded/served.
COPY --from=frontend /web/static ./web/static
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/flagforge ./cmd/flagforge

# --- Runtime stage ---------------------------------------------------------
FROM alpine:3.20
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/flagforge /app/flagforge
COPY --from=build /src/web /app/web
USER app
EXPOSE 9001 7001
ENTRYPOINT ["/app/flagforge"]
