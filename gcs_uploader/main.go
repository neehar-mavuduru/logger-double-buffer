package gcsuploader

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

const (
	BufferSize = 128 * 1024 * 1024 // 128 MB upload buffer
	ChunkSize  = 32 * 1024 * 1024  // 32 MB chunk for high throughput
)

func main() {
	ctx := context.Background()

	// ------------------------------
	// 1. Create client with gRPC pool
	// ------------------------------
	client, err := storage.NewClient(ctx,
		option.WithGRPCConnectionPool(64),
	)
	if err != nil {
		log.Fatalf("storage client: %v", err)
	}
	defer client.Close()

	// ------------------------------
	// 2. Prepare 128 MB buffer
	// ------------------------------
	buf := make([]byte, BufferSize)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("Error generating random data: %v", err)
	}

	bucket := "gcs-dsci-srch-search-prd"
	object := fmt.Sprintf("Image_search/gcs-flush/poc-grpc-%d.bin", time.Now().UnixNano())

	fmt.Printf("Uploading %d MB buffer using parallel gRPC API...\n", BufferSize/(1024*1024))

	// ------------------------------
	// 3. Upload and measure
	// ------------------------------
	start := time.Now()
	if err := UploadParallel(ctx, client, bucket, object, buf, ChunkSize); err != nil {
		log.Fatalf("Upload error: %v", err)
	}
	total := time.Since(start)

	mb := float64(len(buf)) / (1024 * 1024)
	fmt.Printf("\n======== Summary ========\n")
	fmt.Printf("Object:        gs://%s/%s\n", bucket, object)
	fmt.Printf("Size:          %.2f MB\n", mb)
	fmt.Printf("Total Time:    %v\n", total)
	fmt.Printf("Throughput:    %.2f MB/s\n", mb/total.Seconds())
	fmt.Println("==========================")
}
