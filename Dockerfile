# ROSETTA image. Build: docker build -t rosetta .
# The binary is /app, as the platform's compose file expects
# (allspark apps/_template). Runs as a non-root user on a read-only root file
# system; it writes nothing to disk (logs go to stderr).

FROM golang:1.27.4-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/rosetta

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="ROSETTA" \
      org.opencontainers.image.description="Translation and grammar service of the Eastern Province" \
      org.opencontainers.image.source="https://github.com/Rumeasiyan/rosetta" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/app", "-healthcheck"]
ENTRYPOINT ["/app"]
CMD ["serve"]
