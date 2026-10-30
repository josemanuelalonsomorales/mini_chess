module game-service

go 1.22

require (
	github.com/lib/pq v1.10.9
	google.golang.org/grpc v1.62.1
	google.golang.org/protobuf v1.33.0
	mini-chess/proto/pb v0.0.0
)

replace mini-chess/proto/pb => ../../proto/pb
