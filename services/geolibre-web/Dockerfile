# Build stage
FROM node:20-alpine AS builder
WORKDIR /app

# Clone the official GeoLibre repo
RUN apk add --no-cache git && \
    git clone --depth 1 https://github.com/opengeos/GeoLibre.git .

# Install dependencies and build
RUN npm install
# Enable collaboration feature
ENV VITE_GEOLIBRE_COLLAB_URL=wss://geolibre-collab.inec.gov.ng
RUN npm run build

# Serve stage
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
