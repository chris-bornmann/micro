# Builds mapd into a small image that runs the same way on a laptop and on
# AWS Fargate.
#
# Build from the server directory, which is the build context:
#
#	docker build -t mapd server
#	docker run --rm -p 8080:8080 mapd
#
# mapd's flags go after the image name, or in the task definition's command,
# e.g. -cors-origin https://example.com. They replace the default command, so
# keep -addr :8080 among them.

FROM golang:1.26.5-alpine AS build
WORKDIR /src
# Download modules in their own layer so that a code change doesn't refetch
# them.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Content is embedded in the binary, so it is the only file the image needs.
# CGO is off so that it runs on an image with no libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mapd ./cmd/mapd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mapd /mapd
EXPOSE 8080
ENTRYPOINT ["/mapd"]
CMD ["-addr", ":8080"]
