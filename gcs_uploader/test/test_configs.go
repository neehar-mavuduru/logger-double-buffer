package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"cloud.google.com/go/storage"
	gcsuploader "github.com/neeharmavuduru/logger-double-buffer/gcs_uploader"
	"google.golang.org/api/option"
)

// UploadResult holds the results of a single upload
type UploadResult struct {
	UploadNum  int
	Success    bool
	Duration   time.Duration
	Throughput float64 // MB/s
	Error      error
}

func main() {
	var (
		filePath    = flag.String("file", "", "Path to file to upload (required)")
		chunkSizeMB = flag.Int("chunk", 32, "Chunk size in MB")
		bucket      = flag.String("bucket", "gcs-dsci-srch-search-prd", "GCS bucket name")
		repeats     = flag.Int("repeats", 1, "Number of times to upload the same file")
		verbose     = flag.Bool("verbose", false, "Verbose output")
	)
	flag.Parse()

	if *filePath == "" {
		log.Fatal("Error: -file flag is required. Use -file <path> to specify the file to upload.")
	}

	// Read file
	fileData, err := os.ReadFile(*filePath)
	if err != nil {
		log.Fatalf("Error reading file %s: %v", *filePath, err)
	}

	fileSizeMB := float64(len(fileData)) / (1024 * 1024)
	fmt.Printf("\n======== File Upload Test ========\n")
	fmt.Printf("File:          %s\n", *filePath)
	fmt.Printf("File Size:     %.2f MB\n", fileSizeMB)
	fmt.Printf("Chunk Size:    %d MB\n", *chunkSizeMB)
	fmt.Printf("Repeats:       %d\n", *repeats)
	fmt.Printf("Bucket:        %s\n", *bucket)
	fmt.Println("==================================\n")

	ctx := context.Background()

	// Create client with gRPC pool
	client, err := storage.NewClient(ctx,
		option.WithGRPCConnectionPool(64),
	)
	if err != nil {
		log.Fatalf("storage client: %v", err)
	}
	defer client.Close()

	// Run uploads
	results := runUploads(ctx, client, *bucket, fileData, int64(*chunkSizeMB)*1024*1024, *repeats, *verbose)

	// Print summary
	printSummary(results, fileSizeMB)
}

// runUploads uploads the file data multiple times and returns results
func runUploads(ctx context.Context, client *storage.Client, bucket string, fileData []byte, chunkSize int64, repeats int, verbose bool) []UploadResult {
	results := make([]UploadResult, 0, repeats)
	fileSizeMB := float64(len(fileData)) / (1024 * 1024)

	fmt.Printf("Starting %d upload(s)...\n\n", repeats)

	for i := 0; i < repeats; i++ {
		uploadNum := i + 1
		fmt.Printf("[Upload %d/%d] ", uploadNum, repeats)

		// Generate unique object name
		object := fmt.Sprintf("Image_search/gcs-flush/upload-%d-chunk%dMB-%d.bin",
			time.Now().UnixNano(),
			chunkSize/(1024*1024),
			uploadNum)

		start := time.Now()
		err := gcsuploader.UploadParallel(ctx, client, bucket, object, fileData, int(chunkSize))
		duration := time.Since(start)

		throughput := 0.0
		if duration > 0 {
			throughput = fileSizeMB / duration.Seconds()
		}

		result := UploadResult{
			UploadNum:  uploadNum,
			Success:    err == nil,
			Duration:   duration,
			Throughput: throughput,
			Error:      err,
		}

		if err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
		} else {
			fmt.Printf("✓ Success | Duration: %v | Throughput: %.2f MB/s\n",
				duration, throughput)
		}

		results = append(results, result)

		// Small delay between uploads
		if i < repeats-1 {
			time.Sleep(1 * time.Second)
		}
	}

	return results
}

// printSummary prints a formatted summary of all upload results
func printSummary(results []UploadResult, fileSizeMB float64) {
	fmt.Printf("\n\n")
	fmt.Println("=" + string(make([]byte, 80)) + "=")
	fmt.Println("📊 UPLOAD SUMMARY")
	fmt.Println("=" + string(make([]byte, 80)) + "=")

	fmt.Printf("\n%-10s %15s %15s %12s\n",
		"Upload #", "Duration", "Throughput (MB/s)", "Status")
	fmt.Println(string(make([]byte, 80)))

	successCount := 0
	var totalDuration time.Duration
	var throughputs []float64

	for _, result := range results {
		status := "✓"
		if !result.Success {
			status = "❌"
		} else {
			successCount++
			totalDuration += result.Duration
			throughputs = append(throughputs, result.Throughput)
		}

		fmt.Printf("%-10d %15v %15.2f %12s\n",
			result.UploadNum,
			result.Duration,
			result.Throughput,
			status,
		)

		if result.Error != nil {
			fmt.Printf("  └─ Error: %v\n", result.Error)
		}
	}

	fmt.Println(string(make([]byte, 80)))

	// Calculate statistics
	if successCount > 0 {
		avgDuration := totalDuration / time.Duration(successCount)
		avgThroughput := 0.0
		minThroughput := 0.0
		maxThroughput := 0.0

		if len(throughputs) > 0 {
			sum := 0.0
			minThroughput = throughputs[0]
			maxThroughput = throughputs[0]
			for _, t := range throughputs {
				sum += t
				if t < minThroughput {
					minThroughput = t
				}
				if t > maxThroughput {
					maxThroughput = t
				}
			}
			avgThroughput = sum / float64(len(throughputs))
		}

		fmt.Printf("\nFile Size:        %.2f MB\n", fileSizeMB)
		fmt.Printf("Total Uploads:    %d\n", len(results))
		fmt.Printf("Successful:       %d\n", successCount)
		fmt.Printf("Failed:           %d\n", len(results)-successCount)
		fmt.Printf("\nAverage Duration: %v\n", avgDuration)
		fmt.Printf("Average Throughput: %.2f MB/s\n", avgThroughput)
		if len(throughputs) > 1 {
			fmt.Printf("Min Throughput:   %.2f MB/s\n", minThroughput)
			fmt.Printf("Max Throughput:   %.2f MB/s\n", maxThroughput)
		}
	} else {
		fmt.Printf("\n❌ All uploads failed!\n")
	}
}
