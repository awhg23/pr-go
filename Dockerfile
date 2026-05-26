FROM golang:1.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/pr-go ./cmd/pr-go

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/pr-go /pr-go
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/pr-go", "--server", "--addr", ":8080"]
