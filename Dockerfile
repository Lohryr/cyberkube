# Multi-stage build for the cyberkube backend.
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/cyberkube ./cmd/cyberkube

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/cyberkube /cyberkube
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/cyberkube"]
