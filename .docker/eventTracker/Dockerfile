# Dockerfile References: https://docs.docker.com/engine/reference/builder/

# Start from golang:1.12-alpine base image
FROM golang:1.12-alpine

# The latest alpine images don't have some tools like (`git` and `bash`).
# Adding git, bash and openssh to the image
RUN apk update && apk upgrade && \
    apk add --no-cache bash git openssh


# Set the Current Working Directory inside the container
WORKDIR /app

RUN go get github.com/gorilla/mux
RUN go get github.com/iancoleman/strcase
RUN go get github.com/kshvakov/clickhouse
RUN go get go.mongodb.org/mongo-driver/mongo
COPY ./entrypoint.sh /opt

## Build the Go app
CMD ["sh", "/opt/entrypoint.sh"]
#CMD ["tail", "-f", "/dev/null"]