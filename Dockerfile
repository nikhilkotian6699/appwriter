# syntax=docker/dockerfile:1

# ---- frontend ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN go run ./guide/build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/writersguild ./cmd/writersguild \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fakegateway ./cmd/fakegateway

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/writersguild /writersguild
COPY --from=build /out/fakegateway /fakegateway
EXPOSE 8080
ENTRYPOINT ["/writersguild"]
