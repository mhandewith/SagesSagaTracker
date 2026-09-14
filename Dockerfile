FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
ARG VERSION=dev
ARG REVISION=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" -o /out/server ./cmd/server
RUN mkdir -p /out/data && chmod 700 /out/data

FROM scratch
LABEL org.opencontainers.image.title="SagesSagaTracker"
LABEL org.opencontainers.image.source="https://github.com/mhandewith/SagesSagaTracker"
COPY --from=build /out/server /server
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV PORT=8080
ENV DATA_DIR=/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/server", "healthcheck"]
ENTRYPOINT ["/server"]
