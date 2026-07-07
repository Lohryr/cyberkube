# Build the static bundle, then serve it from nginx.
# VITE_API_BASE is baked at build time (Vite inlines env vars); pass it via
# --build-arg for prod, e.g. https://ctf.rokhnir.dev
FROM node:22 AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
ARG VITE_API_BASE=""
ENV VITE_API_BASE=$VITE_API_BASE
RUN npm run build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
