# Builds mapd, with the web client beside it, into a small image that runs
# the same way on a laptop and on AWS Fargate.
#
# Build from the repository root, which is the build context:
#
#	docker build -t mapd .
#	docker run --rm -p 8080:8080 mapd
#
# The game is then at http://localhost:8080/. mapd's flags go after the image
# name, or in the task definition's command (see deploy/), e.g. -auth cognito.
# They replace the default command, so keep -addr :8080 and -web-dir /web
# among them.

FROM node:24-alpine AS web
WORKDIR /src
# Install packages in their own layer so that a code change doesn't
# reinstall them.
COPY client/web/package.json client/web/package-lock.json ./
RUN npm ci
COPY client/web/ ./
# An empty API URL makes the web client call the server it was loaded from.
RUN VITE_API_URL= npm run build

FROM golang:1.26.5-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# Content is embedded in the binary, so it is the only file the server
# needs. CGO is off so that it runs on an image with no libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mapd ./cmd/mapd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/mapd /mapd
COPY --from=web /src/dist /web
EXPOSE 8080
ENTRYPOINT ["/mapd"]
CMD ["-addr", ":8080", "-web-dir", "/web"]
