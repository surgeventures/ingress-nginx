REPO=514443763038.dkr.ecr.us-east-1.amazonaws.com
IMAGE_NAME=nginx-custom-error-pages
IMAGE_TAG=1.0.0

set -e

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags "-s -w" -o custom-error-pages
docker build -t $REPO/$IMAGE_NAME:$IMAGE_TAG .
$(aws ecr get-login --no-include-email --region us-east-1)
docker push $REPO/$IMAGE_NAME:$IMAGE_TAG
