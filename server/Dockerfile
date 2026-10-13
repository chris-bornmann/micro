# Builds mapd into a small image that runs the same way on a laptop and on
# AWS Lambda.
#
# Build from the server directory, which is the build context:
#
#	docker build -t mapd server
#	docker run --rm -p 8080:8080 mapd
#
# On Lambda, the AWS Lambda Web Adapter (an extension copied in below) turns
# each invocation into a plain HTTP request to mapd on :8080, so mapd needs no
# Lambda-specific code. Outside Lambda the extension never starts and the
# image is just mapd.
#
# mapd's flags go after the image name, or in the function's command
# override on Lambda, e.g. -cors-origin https://example.com. They replace
# the default command, so keep -addr :8080 among them.

ARG LAMBDA_ADAPTER_IMAGE=public.ecr.aws/awsguru/aws-lambda-adapter:0.9.1

FROM ${LAMBDA_ADAPTER_IMAGE} AS adapter

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
COPY --from=adapter /lambda-adapter /opt/extensions/lambda-adapter
COPY --from=build /out/mapd /mapd
# The adapter waits for this path to answer before it forwards any traffic.
# Its default, "/", is a 404 here.
ENV AWS_LWA_PORT=8080 \
    AWS_LWA_READINESS_CHECK_PATH=/healthz
EXPOSE 8080
ENTRYPOINT ["/mapd"]
CMD ["-addr", ":8080"]
