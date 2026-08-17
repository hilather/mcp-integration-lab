# labinfo — lab service directory (MCP streamable HTTP).
# Build context is the repo root (see docker-compose.yaml); .dockerignore
# keeps third_party/, secrets/, and .data/ out of the context.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/labinfo cmd/labinfo
COPY internal/labinfo internal/labinfo
COPY internal/profile internal/profile
COPY internal/envfile internal/envfile
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /labinfo ./cmd/labinfo

FROM scratch
COPY --from=build /labinfo /labinfo
USER 65532:65532
ENTRYPOINT ["/labinfo"]
CMD ["serve"]
