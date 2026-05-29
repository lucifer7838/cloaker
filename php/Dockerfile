FROM php:8.4-fpm-alpine

RUN apk add --no-cache curl && \
    docker-php-ext-install curl

COPY . /var/www/html/

RUN chown -R www-data:www-data /var/www/html

EXPOSE 9000

CMD ["php-fpm"]
