build:
	go get github.com/aws/aws-lambda-go/lambda
	env GOOS=linux go build -ldflags="-s -w" -o bin/spotify_reducer spotify_reducer/import.go spotify_reducer/token.go spotify_reducer/main.go
