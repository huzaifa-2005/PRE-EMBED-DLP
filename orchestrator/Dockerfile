FROM golang:1.22-alpine AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY . .
RUN go mod tidy && go build -o /out/orchestrator .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/orchestrator .
CMD ["./orchestrator"]