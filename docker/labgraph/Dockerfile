# labgraph — LabScenario orchestrator (REST + MCP + SPA).
# Build context is the repo root (see docker-compose.yaml); .dockerignore
# keeps third_party/, secrets/, and .data/ out of the context.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/labgraph cmd/labgraph
COPY internal/labgraph internal/labgraph
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /labgraph ./cmd/labgraph

FROM scratch
COPY --from=build /labgraph /labgraph
USER 65532:65532
ENTRYPOINT ["/labgraph"]
CMD ["serve"]
