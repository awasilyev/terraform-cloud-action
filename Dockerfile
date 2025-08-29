FROM golang:1.24-alpine AS build

RUN apk add --no-cache git
COPY go.mod go.sum /app/
WORKDIR /app
RUN go mod download

COPY . /app
RUN CGO_ENABLED=0 go build -o main

# ---
# Container image that runs your code
FROM alpine:3.14

# Copies your code file from your action repository to the filesystem path `/` of the container
COPY --from=build /app/main /app

# Code file to execute when the docker container starts up (`entrypoint.sh`)
ENTRYPOINT ["/app"]
