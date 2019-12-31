build:
	go get github.com/aws/aws-lambda-go/lambda
	env GOOS=linux go build -ldflags="-s -w" -o bin/reducer reducer/import.go reducer/token.go reducer/main.go
