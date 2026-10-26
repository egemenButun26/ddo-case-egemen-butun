FROM node:20-alpine AS derleme

WORKDIR /uygulama

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine

COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=derleme /uygulama/dist/ /usr/share/nginx/html/

EXPOSE 8080
