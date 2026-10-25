FROM nginxinc/nginx-unprivileged:1.27-alpine

COPY web/ /usr/share/nginx/html/
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080
