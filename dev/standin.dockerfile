# The local stand-ins, for dev/compose.yaml only. Never pushed; the service's own
# image (../Dockerfile) builds ./cmd/rosetta alone.
FROM golang:1.27.3-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/standin ./cmd/rosetta-standin

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/standin /standin
USER nonroot:nonroot
EXPOSE 53190
ENTRYPOINT ["/standin"]
