generate:
    docker run --rm -it -v "${PWD}:/clowder" -w /clowder ghcr.io/go-swagger/go-swagger:latest \
        generate client -f openapi/firecracker.yaml -A firecracker