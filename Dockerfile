FROM golang:latest as build
LABEL maintainer="nsheridan@gmail.com"
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /cf-ddns
RUN ls /go/bin

FROM gcr.io/distroless/static
LABEL maintainer="nsheridan@gmail.com"
COPY --from=build /cf-ddns /cf-ddns
ENTRYPOINT ["/cf-ddns"]
