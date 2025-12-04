package main

import (
	"context"
	"fmt"
	"log"
	"time"

	pb "github.com/neeharmavuduru/logger-double-buffer/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	serverAddr = "localhost:8585"
	timeout    = 5 * time.Second
)

func main() {
	// Set up a connection to the server
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Create a client
	client := pb.NewRandomNumberServiceClient(conn)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Call GetRandomNumbers
	fmt.Println("Calling GetRandomNumbers...")
	response, err := client.GetRandomNumbers(ctx, &pb.GetRandomNumbersRequest{})
	if err != nil {
		log.Fatalf("Failed to call GetRandomNumbers: %v", err)
	}

	// Display the response
	fmt.Println("\n=== Response ===")
	fmt.Printf("Numbers: %s\n", response.Numbers)
	
	// Count and display the numbers
	numbers := len(splitNumbers(response.Numbers))
	fmt.Printf("\nTotal numbers received: %d\n", numbers)
}

// splitNumbers splits the colon-separated numbers string
func splitNumbers(s string) []string {
	if s == "" {
		return []string{}
	}
	return splitString(s, ":")
}

func splitString(s, sep string) []string {
	var result []string
	current := ""
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, current)
			current = ""
			i += len(sep) - 1
		} else {
			current += string(s[i])
		}
	}
	if current != "" || len(result) == 0 {
		result = append(result, current)
	}
	return result
}

