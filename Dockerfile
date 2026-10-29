FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /api .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
